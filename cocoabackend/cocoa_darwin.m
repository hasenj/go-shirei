// Objective-C side of the shirei cocoa backend: an NSWindow + a flipped NSView
// that drives shirei frames. Rasterization is done by shirei's core software
// renderer; this side only blits the resulting BGRA buffer to the window and
// routes window/input/clipboard.
//
// Memory: the window/view/delegate are created once and live for the whole
// program; we deliberately never release them (no ARC, no manual free).
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <IOSurface/IOSurface.h>
#include <string.h>

#include "cocoa.h"
#include "_cgo_export.h"

// -----------------------------------------------------------------------------
//  Present: zero-copy IOSurface + CALayer
//
//  The core software renderer rasterizes directly into an IOSurface's memory; the
//  surface is set as a CALayer's contents, so the window server composites it on
//  the GPU with no per-frame CPU copy.
// -----------------------------------------------------------------------------

void *iosurface_create(int w, int h) {
    NSDictionary *props = @{
        (id)kIOSurfaceWidth:           @(w),
        (id)kIOSurfaceHeight:          @(h),
        (id)kIOSurfaceBytesPerElement: @(4),
        (id)kIOSurfacePixelFormat:     @((unsigned int)'BGRA'), // == kCVPixelFormatType_32BGRA
    };
    return (void *)IOSurfaceCreate((CFDictionaryRef)props);
}

void iosurface_release(void *s) {
    if (s) CFRelease((IOSurfaceRef)s);
}

void *iosurface_lock(void *s, int *outStride) {
    IOSurfaceRef surf = (IOSurfaceRef)s;
    IOSurfaceLock(surf, 0, NULL); // 0 = read/write, cache-coherent
    if (outStride) *outStride = (int)IOSurfaceGetBytesPerRow(surf);
    return IOSurfaceGetBaseAddress(surf);
}

void iosurface_unlock(void *s) {
    IOSurfaceUnlock((IOSurfaceRef)s, 0, NULL);
}

int iosurface_in_use(void *s) {
    return IOSurfaceIsInUse((IOSurfaceRef)s) ? 1 : 0;
}

// -----------------------------------------------------------------------------
//  Window, view, app loop
// -----------------------------------------------------------------------------

@interface ShireiView : NSView <NSTextInputClient> {
    NSMutableAttributedString *markedText;
    NSRange markedSel;
    BOOL loggedReplacementRange;
}
@end

static NSWindow  *gWindow = nil;
static ShireiView *gView  = nil;
static BOOL gWantsFrame = YES;

// gContentLayer is a sublayer we fully own; setting its contents to an IOSurface
// presents the frame.
static CALayer *gContentLayer = nil;

// Keep rendering for a short window after the last input event, so the result
// of an interaction reliably reaches the screen (the timer-driven repaints in
// this window are the same path that drives the example's blinking cursor) and
// previous-frame-dependent layouts settle. Outside this window, with no pending
// animation, the app idles and stops drawing — keeping idle CPU low.
static CFAbsoluteTime gLastInputTime = 0;
static const CFAbsoluteTime kInputRenderWindow = 0.5;

@implementation ShireiView
- (NSMutableAttributedString *)markedTextStorage {
    if (!markedText) {
        markedText = [[NSMutableAttributedString alloc] initWithString:@""];
        markedSel = NSMakeRange(NSNotFound, 0);
    }
    return markedText;
}

- (NSString *)plainString:(id)string {
    if ([string isKindOfClass:[NSAttributedString class]]) {
        return [(NSAttributedString *)string string];
    }
    if ([string isKindOfClass:[NSString class]]) {
        return (NSString *)string;
    }
    return [string description];
}

- (NSRange)clampedMarkedSelection:(NSRange)sel forString:(NSString *)s {
    if (sel.location == NSNotFound) {
        return NSMakeRange(s.length, 0);
    }
    NSUInteger start = MIN(sel.location, s.length);
    NSUInteger end = MIN(start + sel.length, s.length);
    return NSMakeRange(start, end - start);
}

- (void)notifyComposition:(NSString *)s selectedRange:(NSRange)sel {
    NSRange clamped = [self clampedMarkedSelection:sel forString:s];
    shireiSetComposition((char *)[s UTF8String],
                         (int)clamped.location,
                         (int)(clamped.location + clamped.length));
    [self noteInput];
}

- (void)discardMarked {
    if (![self hasMarkedText]) return;
    [[self markedTextStorage] setAttributedString:[[[NSAttributedString alloc] initWithString:@""] autorelease]];
    markedSel = NSMakeRange(NSNotFound, 0);
    shireiSetComposition((char *)"", 0, 0);
    [self noteInput];
}

- (void)commitMarked {
    if (![self hasMarkedText]) return;
    NSString *s = [NSString stringWithString:[[self markedTextStorage] string]];
    [self discardMarked];
    if (s.length > 0) {
        shireiCommitText((char *)[s UTF8String]);
        [self noteInput];
    }
}

- (void)commitMarkedForInterruption {
    if (![self hasMarkedText]) return;
    [self commitMarked];
    [[self inputContext] discardMarkedText];

    // Commit before delivering the click/resign event. Otherwise the next shirei
    // frame would see both committed text and a focus-changing click, and the
    // text could be inserted into the newly clicked field.
    shireiProduceFrame((double)self.bounds.size.width,
                       (double)self.bounds.size.height);
}

- (BOOL)isFlipped { return YES; }                 // top-left origin, y down
- (BOOL)acceptsFirstResponder { return YES; }
// Deliver the first click to the view even when the window isn't yet key.
// Without this, clicking an inactive window only activates it and swallows the
// click — the classic macOS "have to click twice" behaviour, which shows up
// when the app is launched from a terminal and isn't the active app.
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }

- (void)drawRect:(NSRect)dirtyRect {
    // AppKit drives drawRect only for the initial display and resizes; produce a
    // frame if needed, then present it into the content layer. Normal frames are
    // driven by the display link (renderFrame), not drawRect.
    double w = (double)self.bounds.size.width;
    double h = (double)self.bounds.size.height;
    if (shireiNeedsProduce(w, h)) shireiProduceFrame(w, h);
    shireiRenderAndPresent(1); // 1 = presentExpose (out-of-band AppKit repaint)
}

- (void)renderFrame {
    // Render into an IOSurface and set it as the layer's contents; the window
    // server composites it. A static frame presents nothing (the layer keeps
    // showing the last surface), so idle costs nothing.
    shireiProduceFrame((double)self.bounds.size.width,
                       (double)self.bounds.size.height);
    shireiRenderAndPresent(0); // 0 = presentTick (the display-link cadence)
}

// Input is data, not events (shirei's model). Handlers update shirei's input
// state and then call this only to (a) note the input time so the render loop
// stays awake, and (b) request a frame. They do NOT render: the CADisplayLink
// renders every frame from the latest data. Rendering per input event -- in
// particular a full ~6ms produce+paint for every scroll event -- saturated the
// runloop and starved the display link to ~2-4fps; that was the choppy scroll.
- (void)noteInput {
    gLastInputTime = CFAbsoluteTimeGetCurrent();
    gWantsFrame = YES;
}

// Driven by a CADisplayLink (see cocoa_run). Render while there is something to
// do: a pending animation (gWantsFrame), or recent input still settling. Otherwise
// idle (the link still ticks, but this skips the work). sender is unused, so it is
// typed id to serve both CADisplayLink and the NSTimer fallback.
- (void)tick:(id)sender {
    BOOL recentInput = (CFAbsoluteTimeGetCurrent() - gLastInputTime) < kInputRenderWindow;
    if (gWantsFrame || recentInput || shireiFrameRequested()) {
        [self renderFrame];
    }
}

// ---- input ----

- (void)updateTrackingAreas {
    for (NSTrackingArea *ta in [self.trackingAreas copy]) {
        [self removeTrackingArea:ta];
    }
    NSTrackingArea *ta = [[NSTrackingArea alloc]
        initWithRect:self.bounds
             options:(NSTrackingMouseMoved | NSTrackingActiveInKeyWindow | NSTrackingInVisibleRect)
               owner:self
            userInfo:nil];
    [self addTrackingArea:ta];
    [super updateTrackingAreas];
}

- (NSPoint)viewPoint:(NSEvent *)e {
    return [self convertPoint:e.locationInWindow fromView:nil];
}

- (void)mouseMoved:(NSEvent *)e {
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 0, 0);
    [self noteInput];
}
- (void)mouseDragged:(NSEvent *)e {
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 3, 0);
    [self noteInput];
}
- (void)mouseDown:(NSEvent *)e {
    // Safety net for IME engagement: if a click lands while our window is NOT key,
    // make it key now so AppKit binds our text input context before the user starts
    // typing. This can happen when launch activation lost its race and left the app
    // frontmost-but-not-active, made worse by acceptsFirstMouse: delivering the click
    // without the usual activate-on-click. Without a key window,
    // +[NSTextInputContext currentInputContext] is nil, IMK has no client, and a
    // Japanese IME silently falls back to raw Latin. No-op once we are already key.
    if (self.window && !self.window.isKeyWindow) {
        [NSApp activateIgnoringOtherApps:YES];
        [self.window makeKeyAndOrderFront:nil];
    }
    [self commitMarkedForInterruption];
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 1, 0);
    [self noteInput];
}
- (void)mouseUp:(NSEvent *)e {
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 2, 0);
    [self noteInput];
}
- (void)rightMouseDown:(NSEvent *)e {
    [self commitMarkedForInterruption];
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 1, 1);
    [self noteInput];
}
- (void)rightMouseUp:(NSEvent *)e {
    NSPoint p = [self viewPoint:e];
    shireiMouse(p.x, p.y, 2, 1);
    [self noteInput];
}

- (void)scrollWheel:(NSEvent *)e {
    // Trackpad pinch is reported to plain NSViews (ones that don't implement
    // magnifyWithEvent:) as a scroll event with the Control modifier set, not
    // a separate gesture event — and that synthetic modifier doesn't come
    // with its own flagsChanged, so it'd never reach shirei via that path.
    // Sync from the scroll event's own modifierFlags to catch it.
    shireiSetModifiers((unsigned)e.modifierFlags);

    double dx = e.scrollingDeltaX;
    double dy = e.scrollingDeltaY;
    if (!e.hasPreciseScrollingDeltas) {
        dx *= 10.0; // line deltas -> approx points
        dy *= 10.0;
    }
    // NOTE: sign chosen to match shirei's scroll convention; verify direction
    // interactively and flip if natural-scroll feels inverted.
    shireiScroll(-dx, -dy);
    [self noteInput];
}

- (void)flagsChanged:(NSEvent *)e {
    shireiSetModifiers((unsigned)e.modifierFlags);
    [self noteInput];
}

- (void)keyDown:(NSEvent *)e {
    shireiSetModifiers((unsigned)e.modifierFlags);
    const char *bare  = e.charactersIgnoringModifiers.length
                            ? [e.charactersIgnoringModifiers UTF8String] : "";

    NSEventModifierFlags mods = e.modifierFlags & NSEventModifierFlagDeviceIndependentFlagsMask;
    if ((mods & NSEventModifierFlagCommand) || (mods & NSEventModifierFlagControl)) {
        // Shortcuts bypass the text input context. interpretKeyEvents: on chords
        // can produce selector noise, while shirei already handles Cmd/Ctrl keys.
        shireiKeyDown((int)e.keyCode, (char *)bare);
        [self noteInput];
        return;
    }

    if (![self hasMarkedText]) {
        shireiKeyDown((int)e.keyCode, (char *)bare);
    }
    [self interpretKeyEvents:@[e]];
    [self noteInput];
}

- (void)insertText:(id)string {
    [self insertText:string replacementRange:NSMakeRange(NSNotFound, 0)];
}

- (void)insertText:(id)string replacementRange:(NSRange)replacementRange {
    [self discardMarked];
    NSString *s = [self plainString:string];
    if (s.length > 0) {
        shireiCommitText((char *)[s UTF8String]);
        [self noteInput];
    }
}

- (void)setMarkedText:(id)string selectedRange:(NSRange)selectedRange replacementRange:(NSRange)replacementRange {
    if (replacementRange.location != NSNotFound && !loggedReplacementRange) {
        NSLog(@"shirei: NSTextInputClient replacementRange unsupported in v1: location=%lu length=%lu",
              (unsigned long)replacementRange.location, (unsigned long)replacementRange.length);
        loggedReplacementRange = YES;
    }

    NSString *s = [self plainString:string];
    if (s.length == 0) {
        [self discardMarked];
        return;
    }

    [[self markedTextStorage] setAttributedString:[[[NSAttributedString alloc] initWithString:s] autorelease]];
    markedSel = [self clampedMarkedSelection:selectedRange forString:s];
    [self notifyComposition:s selectedRange:markedSel];
}

- (void)unmarkText {
    [self commitMarked];
}

- (BOOL)hasMarkedText {
    return markedText && markedText.length > 0;
}

- (NSRange)markedRange {
    if (![self hasMarkedText]) return NSMakeRange(NSNotFound, 0);
    return NSMakeRange(0, markedText.length);
}

- (NSRange)selectedRange {
    if (![self hasMarkedText]) {
        // Even though shirei's v1 IME bridge does not expose the document
        // selection to AppKit, Japanese IMEs may ask selectedRange before
        // starting a fresh marked-text session. Returning NSNotFound can make
        // them fall back to direct Latin insertText after caret navigation.
        return NSMakeRange(0, 0);
    }
    return markedSel;
}

- (NSAttributedString *)attributedSubstringForProposedRange:(NSRange)range actualRange:(NSRangePointer)actualRange {
    return nil;
}

- (NSUInteger)characterIndexForPoint:(NSPoint)point {
    return NSNotFound;
}

- (NSRect)firstRectForCharacterRange:(NSRange)range actualRange:(NSRangePointer)actualRange {
    if (actualRange) *actualRange = range;
    CGFloat x = (CGFloat)shireiCaretX();
    CGFloat y = (CGFloat)shireiCaretY();
    CGFloat h = (CGFloat)shireiCaretHeight();
    if (h <= 0) h = 16;

    // shirei.CaretPos is the caret's bottom-left in this flipped view's logical
    // coordinates. Convert the caret rect through AppKit so the IME receives the
    // screen-coordinate rect it expects.
    NSRect viewRect = NSMakeRect(x, y - h, 1, h);
    NSRect windowRect = [self convertRect:viewRect toView:nil];
    return self.window ? [self.window convertRectToScreen:windowRect] : windowRect;
}

- (NSArray<NSAttributedStringKey> *)validAttributesForMarkedText {
    return @[];
}

- (void)doCommandBySelector:(SEL)selector {
    // interpretKeyEvents: maps arrows/enter/etc. to selectors, but shirei's
    // virtual-key relay already delivered those keys. Acting here would
    // double-deliver them; calling super only beeps.
}

- (void)keyUp:(NSEvent *)e {
    const char *bare = e.charactersIgnoringModifiers.length
                           ? [e.charactersIgnoringModifiers UTF8String] : "";
    shireiKeyUp((int)e.keyCode, (char *)bare);
    [self noteInput];
}

- (BOOL)resignFirstResponder {
    [self commitMarkedForInterruption];
    return [super resignFirstResponder];
}
@end

@interface ShireiAppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate>
@end

@implementation ShireiAppDelegate
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

- (void)windowDidResignKey:(NSNotification *)notification {
    [gView commitMarkedForInterruption];
    shireiWindowFocus(0);
}

- (void)windowDidBecomeKey:(NSNotification *)notification {
    shireiWindowFocus(1);
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    // Activate the app and key the window AFTER the run loop is up. The window is
    // already created and ordered front in cocoa_setupWindow, but activating there
    // (or in cocoa_run before -[NSApp run]) is unreliable for a terminal-launched
    // UNBUNDLED binary (go run, bare executable): the process becomes frontmost yet
    // the app is often left NOT active with a NON-key window. In that state AppKit
    // never activates our view's text input context, so
    // +[NSTextInputContext currentInputContext] stays nil and IMK has no client to
    // compose into -- a Japanese IME then silently emits raw Latin, latched until the
    // app is switched away and back (which finally fires windowDidBecomeKey). This
    // delegate callback runs once the run loop is servicing events, where activation
    // reliably sticks and the window becomes key. Fixes the intermittent IME
    // Latin-fallback bug; see notes/ime-plan.md.
    [NSApp activateIgnoringOtherApps:YES];
    [gWindow makeKeyAndOrderFront:nil];
}
@end

void cocoa_setupWindow(const char *title, int width, int height) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        [NSApp setDelegate:[[ShireiAppDelegate alloc] init]];

        NSRect frame = NSMakeRect(0, 0, width, height);
        NSUInteger style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                           NSWindowStyleMaskResizable | NSWindowStyleMaskMiniaturizable;
        gWindow = [[NSWindow alloc] initWithContentRect:frame
                                              styleMask:style
                                                backing:NSBackingStoreBuffered
                                                  defer:NO];
        [gWindow setTitle:[NSString stringWithUTF8String:title]];

        gView = [[ShireiView alloc] initWithFrame:frame];
        [gWindow setContentView:gView];
        [gWindow setDelegate:(id<NSWindowDelegate>)[NSApp delegate]];
        [gWindow setAcceptsMouseMovedEvents:YES];
        [gWindow makeFirstResponder:gView];
        [gWindow center];
        [gWindow makeKeyAndOrderFront:nil];
    }
}

// cocoa_setAppIcon sets the Dock/Cmd-Tab icon. macOS windows have no title-bar
// icon, so the Dock icon is the whole story here. Works for plain (unbundled)
// executables, which otherwise get the generic terminal-app icon. Best-effort:
// an unreadable file leaves the default icon.
void cocoa_setAppIcon(const char *path) {
    @autoreleasepool {
        NSImage *img = [[NSImage alloc]
            initWithContentsOfFile:[NSString stringWithUTF8String:path]];
        if (img) [NSApp setApplicationIconImage:img];
    }
}

// cocoa_setAppIconRGBA: same as cocoa_setAppIcon but from raw straight-alpha
// RGBA pixels (stride w*4), for icons supplied as image.Image/[]byte rather
// than a file. The pixels are copied; the caller's buffer is not retained.
void cocoa_setAppIconRGBA(const unsigned char *pix, int w, int h) {
    @autoreleasepool {
        NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
            initWithBitmapDataPlanes:NULL
                          pixelsWide:w
                          pixelsHigh:h
                       bitsPerSample:8
                     samplesPerPixel:4
                            hasAlpha:YES
                            isPlanar:NO
                      colorSpaceName:NSCalibratedRGBColorSpace
                        bitmapFormat:NSBitmapFormatAlphaNonpremultiplied
                         bytesPerRow:w * 4
                        bitsPerPixel:32];
        if (!rep) return;
        memcpy([rep bitmapData], pix, (size_t)w * (size_t)h * 4);
        NSImage *img = [[NSImage alloc] initWithSize:NSMakeSize(w, h)];
        [img addRepresentation:rep];
        [NSApp setApplicationIconImage:img];
    }
}

void cocoa_run(void) {
    // Drive frames from a CADisplayLink, not an NSTimer. NSTimer is low priority
    // and gets starved by the runloop during continuous scroll/drag (measured
    // ~15fps even though a frame costs only ~6ms). CADisplayLink is display-synced
    // and serviced reliably during event tracking, which is exactly what it's for.
    if (@available(macOS 14.0, *)) {
        CADisplayLink *dl = [gView displayLinkWithTarget:gView selector:@selector(tick:)];
        [dl addToRunLoop:[NSRunLoop currentRunLoop] forMode:NSRunLoopCommonModes];
    } else {
        NSTimer *t = [NSTimer timerWithTimeInterval:1.0 / 60.0
                                             target:gView
                                           selector:@selector(tick:)
                                           userInfo:nil
                                            repeats:YES];
        [[NSRunLoop currentRunLoop] addTimer:t forMode:NSRunLoopCommonModes];
    }
    // Activation is deliberately NOT done here: doing it before -[NSApp run] leaves a
    // terminal-launched unbundled binary frontmost-but-not-active with a non-key
    // window, which breaks IME engagement. It is done in the delegate's
    // applicationDidFinishLaunching: instead, once the run loop is up. See there.
    [NSApp run];
}

void cocoa_requestRedraw(void) {
    gWantsFrame = YES;
    if (gView) [gView setNeedsDisplay:YES];
}

void cocoa_enable_zerocopy(void) {
    if (!gView) return;
    gView.wantsLayer = YES;
    gContentLayer = [CALayer layer];
    gContentLayer.contentsGravity = kCAGravityResize;
    gContentLayer.contentsScale = gWindow ? [gWindow backingScaleFactor] : 1.0;
    gContentLayer.frame = gView.bounds;
    gContentLayer.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
    gContentLayer.opaque = YES;
    [gView.layer addSublayer:gContentLayer];
}

void cocoa_set_layer_contents(void *surface) {
    if (!gContentLayer) return;
    // Disable implicit animation so contents swap immediately (no crossfade).
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    gContentLayer.frame = gView.bounds; // follow resizes
    gContentLayer.contentsScale = gWindow ? [gWindow backingScaleFactor] : 1.0;
    gContentLayer.contents = (id)(IOSurfaceRef)surface;
    [CATransaction commit];
}

void cocoa_setClipboard(const char *s) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    [pb setString:[NSString stringWithUTF8String:s] forType:NSPasteboardTypeString];
}

char *cocoa_getClipboard(void) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    NSString *s = [pb stringForType:NSPasteboardTypeString];
    if (s == nil) return NULL;
    return strdup([s UTF8String]);
}

void cocoa_setWantsFrame(int v) {
    gWantsFrame = v ? YES : NO;
}

double cocoa_backingScaleFactor(void) {
    if (gWindow) return (double)[gWindow backingScaleFactor];
    return 1.0;
}
