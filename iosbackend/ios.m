// Objective-C side of the shirei iOS backend: a full-screen UIView driven by
// CADisplayLink. Rasterization is done by shirei's core software renderer; this
// side presents the BGRA buffer and routes touch + soft-keyboard / IME into Go.
//
// IME (Japanese etc.): UITextInput with a thin document that only mirrors marked
// (preedit) text — same contract as cocoabackend's NSTextInputClient. Marked
// text → shireiSetComposition; commits → shireiInsertText; the app buffer stays
// in Go (TextInput). Visibility is driven by ios_syncKeyboard from
// shirei.WantsKeyboard after each frame.
//
// Content view: full safe-area frame (stable CALayer size). Keyboard overlap
// only shrinks the layout size reported as WindowSize — presenting always uses
// the full view bounds so the last frame is never stretched during keyboard
// animation. Orientation + keyboard share ios_layoutContentView.
#import <UIKit/UIKit.h>
#import <QuartzCore/QuartzCore.h>
#import <GameController/GameController.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

#include "ios.h"
#include "_cgo_export.h"

// Touch phases passed to shireiTouch (mirror ios_ios.go constants).
enum {
	kTouchBegin  = 0,
	kTouchMove   = 1,
	kTouchEnd    = 2,
	kTouchCancel = 3,
};

static UIWindow *gWindow = nil;
static UIView *gView = nil;
static CADisplayLink *gLink = nil;
static BOOL gWantsFrame = YES;
static CFAbsoluteTime gLastInputTime = 0;
static const CFAbsoluteTime kInputRenderWindow = 0.5;

// Last UIKeyboardFrameEndUserInfoKey in screen coordinates. CGRectNull = no
// keyboard (or fully dismissed). Updated by keyboard notifications.
static CGRect gKeyboardFrameScreen;

// Layout size in points (safe area minus keyboard). Drawn content uses this as
// WindowSize; the view/layer stays at full safe-area size (gView.bounds).
static CGFloat gLayoutW = 0;
static CGFloat gLayoutH = 0;

// Preferred interface orientation (shirei.PreferredOrientation): 0 any,
// 1 portrait, 2 landscape. Read by ShireiRootVC; written by ios_syncOrientation.
static int gPreferredOrientation = 0;
static int gAppliedOrientation = -1; // force first apply

// Physical keyboard (GCKeyboard), not soft IME. Updated on connect/disconnect.
static BOOL gHardwareKeyboard = NO;

static void ios_layoutContentView(void);
static void ios_applyOrientation(BOOL force);
static void ios_applyOrientationIfNeeded(void);
static BOOL ios_orientationGeometryReady(void);
static void ios_refreshHardwareKeyboard(void);

// ---- thin UITextInput document positions/ranges (UTF-16 indices) ----

@interface ShireiTextPosition : UITextPosition
@property (nonatomic, assign) NSUInteger index;
+ (instancetype)positionWithIndex:(NSUInteger)index;
@end

@implementation ShireiTextPosition
+ (instancetype)positionWithIndex:(NSUInteger)index {
	ShireiTextPosition *p = [[ShireiTextPosition alloc] init];
	p.index = index;
	return p;
}
@end

@interface ShireiTextRange : UITextRange
@property (nonatomic, assign) NSUInteger startIndex;
@property (nonatomic, assign) NSUInteger endIndex;
+ (instancetype)rangeWithStart:(NSUInteger)start end:(NSUInteger)end;
@end

@implementation ShireiTextRange
+ (instancetype)rangeWithStart:(NSUInteger)start end:(NSUInteger)end {
	ShireiTextRange *r = [[ShireiTextRange alloc] init];
	r.startIndex = start;
	r.endIndex = end;
	return r;
}
- (UITextPosition *)start {
	NSUInteger a = MIN(self.startIndex, self.endIndex);
	return [ShireiTextPosition positionWithIndex:a];
}
- (UITextPosition *)end {
	NSUInteger b = MAX(self.startIndex, self.endIndex);
	return [ShireiTextPosition positionWithIndex:b];
}
- (BOOL)isEmpty {
	return self.startIndex == self.endIndex;
}
@end

// Accessory toolbar → shireiAccessoryAction (keep in sync with ios_ios.go).
enum {
	kAccessoryLeft = 0,
	kAccessoryRight,
	kAccessorySelectAll,
	kAccessoryCopy,
	kAccessoryPaste,
	kAccessoryDone,
};

@interface ShireiView : UIView <UITextInput>
- (void)shireiTick:(CADisplayLink *)link;
- (BOOL)hasMarkedText;
- (void)commitMarkedForInterruption;
- (void)keyboardFrameChanged:(NSNotification *)note;
@end

@interface ShireiRootVC : UIViewController
@end

@implementation ShireiRootVC
- (BOOL)prefersStatusBarHidden {
	return NO;
}

// Defer system edge gestures so touches on chrome near the screen edge reach
// the app immediately (Control Center / notification shade from the top,
// home-indicator swipe from the bottom). Without this, UIKit may hold the
// contact until lift and deliver begin+end in one frame — raw Touches never
// stay Active, and touch-aware widgets miss the press.
// Apps still get the system gesture if the user pulls past the recognition
// threshold; we only stop the OS from gating short taps on edge UI.
- (UIRectEdge)preferredScreenEdgesDeferringSystemGestures {
	return UIRectEdgeAll;
}

- (BOOL)shouldAutorotate {
	return YES;
}
- (UIInterfaceOrientationMask)supportedInterfaceOrientations {
	switch (gPreferredOrientation) {
	case 1: // OrientationPortrait
		return UIInterfaceOrientationMaskPortrait;
	case 2: // OrientationLandscape
		return UIInterfaceOrientationMaskLandscape;
	default: // OrientationAny
		return UIInterfaceOrientationMaskAllButUpsideDown;
	}
}
// Used for the initial presentation when the window becomes key; without this
// the system can still pick portrait for a frame if Info.plist lists it.
- (UIInterfaceOrientation)preferredInterfaceOrientationForPresentation {
	switch (gPreferredOrientation) {
	case 1:
		return UIInterfaceOrientationPortrait;
	case 2:
		// Prefer home-button/indicator on the right (common game/app default).
		return UIInterfaceOrientationLandscapeRight;
	default:
		return UIInterfaceOrientationPortrait;
	}
}
- (void)viewDidLoad {
	[super viewDidLoad];
	self.view.backgroundColor = [UIColor blackColor];
}
- (void)viewDidLayoutSubviews {
	[super viewDidLayoutSubviews];
	// Rotation, split, safe-area insets: recompute content frame (keyboard-aware).
	ios_layoutContentView();
}
@end

static UIInterfaceOrientationMask ios_orientationMask(int preferred) {
	switch (preferred) {
	case 1:
		return UIInterfaceOrientationMaskPortrait;
	case 2:
		return UIInterfaceOrientationMaskLandscape;
	default:
		return UIInterfaceOrientationMaskAllButUpsideDown;
	}
}

// True when the content view's aspect matches Host.PreferredOrientation
// (or preference is Any). Used to avoid painting a portrait layout for one
// frame before the OS finishes rotating to landscape.
static BOOL ios_orientationGeometryReady(void) {
	if (gPreferredOrientation == 0) {
		return YES;
	}
	CGFloat w = 0, h = 0;
	if (gLayoutW > 0 && gLayoutH > 0) {
		w = gLayoutW;
		h = gLayoutH;
	} else if (gView) {
		w = gView.bounds.size.width;
		h = gView.bounds.size.height;
	} else if (gWindow) {
		w = gWindow.bounds.size.width;
		h = gWindow.bounds.size.height;
	}
	if (w < 1 || h < 1) {
		return NO;
	}
	if (gPreferredOrientation == 2) { // landscape
		return w >= h;
	}
	if (gPreferredOrientation == 1) { // portrait
		return h >= w;
	}
	return YES;
}

// Ask UIKit to honor the current preferred mask (and rotate if the device
// already sits in a now-allowed orientation). Retries after attach when the
// preference was set before the window existed. force re-requests even if we
// already applied the same preference (geometry may still be wrong).
static void ios_applyOrientation(BOOL force) {
	if (!force && gPreferredOrientation == gAppliedOrientation) {
		return;
	}
	if (!gWindow || !gWindow.rootViewController) {
		return; // leave gAppliedOrientation unset so we apply after attach
	}
	UIViewController *vc = gWindow.rootViewController;
	gAppliedOrientation = gPreferredOrientation;
	UIInterfaceOrientationMask mask = ios_orientationMask(gPreferredOrientation);
	if (@available(iOS 16.0, *)) {
		UIWindowScene *scene = vc.view.window.windowScene;
		if (scene == nil && gWindow.windowScene != nil) {
			scene = gWindow.windowScene;
		}
		if (scene) {
			UIWindowSceneGeometryPreferencesIOS *prefs =
			    [[UIWindowSceneGeometryPreferencesIOS alloc]
			        initWithInterfaceOrientations:mask];
			[scene requestGeometryUpdateWithPreferences:prefs errorHandler:nil];
		}
		[vc setNeedsUpdateOfSupportedInterfaceOrientations];
	} else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
		[UIViewController attemptRotationToDeviceOrientation];
#pragma clang diagnostic pop
	}
	gWantsFrame = YES;
	gLastInputTime = CFAbsoluteTimeGetCurrent();
}

static void ios_applyOrientationIfNeeded(void) {
	ios_applyOrientation(NO);
}

void ios_syncOrientation(int preferred) {
	if (preferred < 0) {
		preferred = 0;
	}
	if (preferred > 2) {
		preferred = 0;
	}
	if (preferred != gPreferredOrientation) {
		gAppliedOrientation = -1; // force re-apply when preference changes
	}
	gPreferredOrientation = preferred;
	ios_applyOrientationIfNeeded();
}

static void ios_refreshHardwareKeyboard(void) {
	BOOL was = gHardwareKeyboard;
	// iOS 14+: GCKeyboard.coalescedKeyboard is non-nil while any hardware
	// keyboard is connected. Soft/IME keyboards do not create a GCKeyboard.
	if (@available(iOS 14.0, *)) {
		gHardwareKeyboard = (GCKeyboard.coalescedKeyboard != nil);
	} else {
		gHardwareKeyboard = NO;
	}
	if (gHardwareKeyboard != was) {
		gWantsFrame = YES;
		gLastInputTime = CFAbsoluteTimeGetCurrent();
	}
}

int ios_hardwareKeyboard(void) {
	return gHardwareKeyboard ? 1 : 0;
}

// Full safe-area frame for gView (layer size stable). Layout size = safe area
// height above the keyboard — reported via ios_viewSize / WindowSize.
// Keyboard end frames already include inputAccessoryView height when attached.
static void ios_layoutContentView(void) {
	if (!gWindow || !gView) {
		return;
	}
	UIViewController *vc = gWindow.rootViewController;
	if (!vc.isViewLoaded) {
		return;
	}
	UIView *host = vc.view;
	CGRect safe = host.safeAreaLayoutGuide.layoutFrame;
	if (safe.size.width <= 0 || safe.size.height <= 0) {
		return;
	}

	// Drawable: always the full safe area so CALayer.bounds does not change when
	// the keyboard animates (mismatched contents would stretch under default gravity).
	CGRect oldFrame = gView.frame;
	BOOL viewResized = !CGRectEqualToRect(oldFrame, safe);
	if (viewResized) {
		gView.frame = safe;
		gWantsFrame = YES;
		gLastInputTime = CFAbsoluteTimeGetCurrent();
	}

	CGFloat width = CGRectGetWidth(safe);
	CGFloat height = CGRectGetHeight(safe);
	if (!CGRectIsNull(gKeyboardFrameScreen) && gKeyboardFrameScreen.size.height > 0 &&
	    host.window != nil) {
		CGRect kbInHost =
		    [host convertRect:gKeyboardFrameScreen
		  fromCoordinateSpace:host.window.screen.coordinateSpace];
		// Prefer intersection with the content view (same space as touches).
		CGRect kbInView = [gView convertRect:kbInHost fromView:host];
		CGRect viewBounds = gView.bounds;
		CGRect overlap = CGRectIntersection(viewBounds, kbInView);
		if (!CGRectIsNull(overlap) && !CGRectIsEmpty(overlap)) {
			height = MAX((CGFloat)0, CGRectGetMinY(overlap));
		}
	}

	BOOL layoutResized = (gLayoutW != width || gLayoutH != height);
	if (layoutResized) {
		gLayoutW = width;
		gLayoutH = height;
		gWantsFrame = YES;
		gLastInputTime = CFAbsoluteTimeGetCurrent();
	}

	// Stale CALayer.contents: we are not a classic double-buffer swapchain, but
	// the previous CGImage stays on the layer until the next produce. If the
	// view/layout size changed first, Core Animation shows that old image
	// scaled into the new bounds for one refresh (launch height glitch). Drop
	// contents immediately; next tick paints at the new size.
	if ((viewResized || layoutResized) && gView.layer.contents != nil) {
		gView.layer.contents = nil;
	}
}

@implementation ShireiView {
	// Thin document: empty when not composing; equals marked string while
	// composing. Does not mirror the Go TextInput buffer.
	NSMutableString *_doc;
	// Selection in _doc (UTF-16). When not composing, both 0.
	NSUInteger _selStart;
	NSUInteger _selEnd;
	// Marked range in _doc; NSNotFound location means no marked text.
	NSRange _markedRange;
	id<UITextInputTokenizer> _tokenizer;
	UIView *_inputAccessory;
}

@synthesize inputDelegate = _inputDelegate;
@synthesize markedTextStyle = _markedTextStyle;

- (instancetype)initWithFrame:(CGRect)frame {
	self = [super initWithFrame:frame];
	if (self) {
		self.multipleTouchEnabled = YES; // raw multi-touch → InputState.Touches
		self.opaque = YES;
		self.backgroundColor = [UIColor blackColor];
		self.contentScaleFactor = [UIScreen mainScreen].scale;
		// Disable implicit Core Animation on contents swaps (we set a new
		// CGImage every frame). Gravity resize is default; combined with a
		// stale image after bounds change it stretches the previous frame —
		// ios_layoutContentView clears contents on resize to avoid that.
		self.layer.actions = @{@"contents" : [NSNull null]};
		_doc = [NSMutableString string];
		_selStart = 0;
		_selEnd = 0;
		_markedRange = NSMakeRange(NSNotFound, 0);
		_tokenizer = [[UITextInputStringTokenizer alloc] initWithTextInput:self];
	}
	return self;
}

- (void)noteInput {
	gLastInputTime = CFAbsoluteTimeGetCurrent();
}

- (NSString *)plainString:(id)string {
	if ([string isKindOfClass:[NSAttributedString class]]) {
		return [(NSAttributedString *)string string];
	}
	if ([string isKindOfClass:[NSString class]]) {
		return (NSString *)string;
	}
	return [string description] ?: @"";
}

- (BOOL)hasMarkedText {
	return _markedRange.location != NSNotFound && _markedRange.length > 0;
}

- (NSRange)clampedSelection:(NSRange)sel forLength:(NSUInteger)len {
	if (sel.location == NSNotFound) {
		return NSMakeRange(len, 0);
	}
	NSUInteger start = MIN(sel.location, len);
	NSUInteger end = MIN(start + sel.length, len);
	return NSMakeRange(start, end - start);
}

- (void)notifyCompositionFromDoc {
	if (![self hasMarkedText]) {
		shireiClearComposition();
		return;
	}
	NSString *s = [_doc copy];
	NSRange sel = [self clampedSelection:NSMakeRange(_selStart, _selEnd - _selStart) forLength:s.length];
	// selectedRange for composition is the IME clause/caret inside marked text.
	// When the whole document is marked, offsets are absolute in s.
	const char *utf8 = s.UTF8String;
	if (!utf8) {
		utf8 = "";
	}
	shireiSetComposition((char *)utf8, (int)sel.location, (int)(sel.location + sel.length));
}

- (void)clearMarkedLocal {
	[_doc setString:@""];
	_markedRange = NSMakeRange(NSNotFound, 0);
	_selStart = 0;
	_selEnd = 0;
}

// Commit current marked string into Go, then clear local + Composition.
- (void)commitMarked {
	if (![self hasMarkedText]) {
		return;
	}
	NSString *s = [_doc copy];
	[self.inputDelegate textWillChange:self];
	[self.inputDelegate selectionWillChange:self];
	[self clearMarkedLocal];
	shireiClearComposition();
	if (s.length > 0) {
		const char *utf8 = s.UTF8String;
		if (utf8) {
			shireiInsertText((char *)utf8);
		}
	}
	[self.inputDelegate selectionDidChange:self];
	[self.inputDelegate textDidChange:self];
	[self noteInput];
}

// Like Cocoa: flush marked into the still-focused field before a click can
// move focus (produce a frame so FrameInput.Text lands first).
- (void)commitMarkedForInterruption {
	if (![self hasMarkedText]) {
		return;
	}
	[self commitMarked];
	double w = 0, h = 0;
	ios_viewSize(&w, &h);
	if (w > 0 && h > 0) {
		shireiProduceAndPresent(w, h);
	}
}

- (void)forwardTouches:(NSSet<UITouch *> *)touches phase:(int)phase {
	// Each UITouch object identity is stable for the contact lifetime (osKey).
	for (UITouch *t in touches) {
		CGPoint p = [t locationInView:self];
		shireiTouch(p.x, p.y, phase, (uintptr_t)(__bridge void *)t);
	}
	if (touches.count > 0) {
		[self noteInput];
	}
}

- (void)touchesBegan:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	// Commit preedit before focus-changing taps (Cocoa commitMarkedForInterruption).
	[self commitMarkedForInterruption];
	[self forwardTouches:touches phase:kTouchBegin];
}
- (void)touchesMoved:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	[self forwardTouches:touches phase:kTouchMove];
}
- (void)touchesEnded:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	[self forwardTouches:touches phase:kTouchEnd];
}
- (void)touchesCancelled:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	[self forwardTouches:touches phase:kTouchCancel];
}

// ---- first responder / traits ----

- (BOOL)canBecomeFirstResponder {
	return YES;
}

- (BOOL)resignFirstResponder {
	[self commitMarked];
	return [super resignFirstResponder];
}

// Bar above the soft keyboard: full-height segment buttons with light chrome
// and hairline separators (not plain link-style UIBarButtonItem titles).
// Actions are synthetic FrameInput keys in Go; Done clears focus so
// WantsKeyboard drops. Height is included in UIKeyboard frame notifications.
- (UIView *)inputAccessoryView {
	if (_inputAccessory) {
		return _inputAccessory;
	}

	const CGFloat barH = 44;
	UIView *bar = [[UIView alloc] initWithFrame:CGRectMake(0, 0, 320, barH)];
	// Light system-like fill so buttons read as chrome, not text links.
	if (@available(iOS 13.0, *)) {
		bar.backgroundColor = [UIColor secondarySystemBackgroundColor];
	} else {
		bar.backgroundColor = [UIColor colorWithWhite:0.94 alpha:1];
	}
	// Top hairline
	UIView *topLine = [[UIView alloc] initWithFrame:CGRectZero];
	topLine.translatesAutoresizingMaskIntoConstraints = NO;
	if (@available(iOS 13.0, *)) {
		topLine.backgroundColor = [UIColor separatorColor];
	} else {
		topLine.backgroundColor = [UIColor colorWithWhite:0.75 alpha:1];
	}
	[bar addSubview:topLine];

	UIStackView *stack = [[UIStackView alloc] initWithFrame:CGRectZero];
	stack.translatesAutoresizingMaskIntoConstraints = NO;
	stack.axis = UILayoutConstraintAxisHorizontal;
	stack.alignment = UIStackViewAlignmentFill;
	stack.distribution = UIStackViewDistributionFill;
	stack.spacing = 0;
	[bar addSubview:stack];

	UIColor *titleColor = nil;
	UIColor *doneColor = nil;
	UIColor *sepColor = nil;
	UIFont *font = [UIFont systemFontOfSize:15 weight:UIFontWeightRegular];
	UIFont *doneFont = [UIFont systemFontOfSize:15 weight:UIFontWeightSemibold];
	if (@available(iOS 13.0, *)) {
		titleColor = [UIColor labelColor];
		doneColor = [UIColor systemBlueColor];
		sepColor = [UIColor separatorColor];
	} else {
		titleColor = [UIColor darkTextColor];
		doneColor = [UIColor colorWithRed:0 green:0.478 blue:1 alpha:1];
		sepColor = [UIColor colorWithWhite:0.75 alpha:1];
	}

	UIButton *(^makeBtn)(NSString *, int, BOOL) = ^(NSString *title, int action, BOOL emphasized) {
		UIButton *b = [UIButton buttonWithType:UIButtonTypeSystem];
		b.tag = action;
		[b setTitle:title forState:UIControlStateNormal];
		b.titleLabel.font = emphasized ? doneFont : font;
		b.titleLabel.adjustsFontSizeToFitWidth = YES;
		b.titleLabel.minimumScaleFactor = 0.75;
		[b setTitleColor:(emphasized ? doneColor : titleColor) forState:UIControlStateNormal];
		// Slight fill on press so full-height cells feel tappable.
		if (@available(iOS 13.0, *)) {
			[b setBackgroundImage:[self shireiAccessoryImageWithColor:[UIColor tertiarySystemFillColor]]
			             forState:UIControlStateHighlighted];
		} else {
			[b setBackgroundImage:[self shireiAccessoryImageWithColor:[UIColor colorWithWhite:0.85 alpha:1]]
			             forState:UIControlStateHighlighted];
		}
		[b addTarget:self action:@selector(accessoryTapped:) forControlEvents:UIControlEventTouchUpInside];
		// Intrinsic width from title + padding; stack fills height.
		CGFloat pad = emphasized ? 14 : 10;
		b.contentEdgeInsets = UIEdgeInsetsMake(0, pad, 0, pad);
		[b setContentHuggingPriority:UILayoutPriorityDefaultLow forAxis:UILayoutConstraintAxisHorizontal];
		[b setContentCompressionResistancePriority:UILayoutPriorityDefaultLow
		                                   forAxis:UILayoutConstraintAxisHorizontal];
		return b;
	};

	void (^addSep)(void) = ^{
		UIView *sep = [[UIView alloc] initWithFrame:CGRectZero];
		sep.backgroundColor = sepColor;
		sep.translatesAutoresizingMaskIntoConstraints = NO;
		[sep.widthAnchor constraintEqualToConstant:1.0 / UIScreen.mainScreen.scale].active = YES;
		// Inset separators slightly from top/bottom so they read as | not full rules.
		UIView *wrap = [[UIView alloc] initWithFrame:CGRectZero];
		wrap.translatesAutoresizingMaskIntoConstraints = NO;
		[wrap addSubview:sep];
		[wrap.widthAnchor constraintEqualToConstant:1.0 / UIScreen.mainScreen.scale].active = YES;
		[NSLayoutConstraint activateConstraints:@[
			[sep.topAnchor constraintEqualToAnchor:wrap.topAnchor constant:8],
			[sep.bottomAnchor constraintEqualToAnchor:wrap.bottomAnchor constant:-8],
			[sep.centerXAnchor constraintEqualToAnchor:wrap.centerXAnchor],
			[sep.widthAnchor constraintEqualToConstant:1.0 / UIScreen.mainScreen.scale],
		]];
		// Don't flex separators.
		[wrap setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
		[wrap setContentCompressionResistancePriority:UILayoutPriorityRequired
		                                      forAxis:UILayoutConstraintAxisHorizontal];
		[stack addArrangedSubview:wrap];
	};

	// Compact nav pair, then edit actions, Done last (emphasized).
	[stack addArrangedSubview:makeBtn(@"←", kAccessoryLeft, NO)];
	addSep();
	[stack addArrangedSubview:makeBtn(@"→", kAccessoryRight, NO)];
	addSep();
	// Flexible spacer pushes edit cluster + Done toward a balanced layout:
	// give middle buttons equal share of remaining width.
	UIView *spacer = [[UIView alloc] initWithFrame:CGRectZero];
	[spacer setContentHuggingPriority:1 forAxis:UILayoutConstraintAxisHorizontal];
	[spacer setContentCompressionResistancePriority:1 forAxis:UILayoutConstraintAxisHorizontal];
	[stack addArrangedSubview:spacer];
	addSep();
	[stack addArrangedSubview:makeBtn(@"Select All", kAccessorySelectAll, NO)];
	addSep();
	[stack addArrangedSubview:makeBtn(@"Copy", kAccessoryCopy, NO)];
	addSep();
	[stack addArrangedSubview:makeBtn(@"Paste", kAccessoryPaste, NO)];
	addSep();
	UIButton *done = makeBtn(@"Done", kAccessoryDone, YES);
	[done setContentHuggingPriority:UILayoutPriorityDefaultHigh forAxis:UILayoutConstraintAxisHorizontal];
	[done setContentCompressionResistancePriority:UILayoutPriorityRequired
	                                      forAxis:UILayoutConstraintAxisHorizontal];
	[stack addArrangedSubview:done];

	// Equal width among the main text buttons (not arrows/Done/spacers) is hard
	// in a mixed stack; fill distribution + low hugging on middle buttons is enough.

	[NSLayoutConstraint activateConstraints:@[
		[topLine.topAnchor constraintEqualToAnchor:bar.topAnchor],
		[topLine.leadingAnchor constraintEqualToAnchor:bar.leadingAnchor],
		[topLine.trailingAnchor constraintEqualToAnchor:bar.trailingAnchor],
		[topLine.heightAnchor constraintEqualToConstant:1.0 / UIScreen.mainScreen.scale],

		[stack.topAnchor constraintEqualToAnchor:bar.topAnchor],
		[stack.bottomAnchor constraintEqualToAnchor:bar.bottomAnchor],
		[stack.leadingAnchor constraintEqualToAnchor:bar.leadingAnchor],
		[stack.trailingAnchor constraintEqualToAnchor:bar.trailingAnchor],

		[bar.heightAnchor constraintEqualToConstant:barH],
	]];

	// intrinsicContentSize for keyboard layout: width flexible, fixed height.
	bar.autoresizingMask = UIViewAutoresizingFlexibleWidth;
	_inputAccessory = bar;
	return _inputAccessory;
}

// 1×1 solid color image for UIButton highlighted background.
- (UIImage *)shireiAccessoryImageWithColor:(UIColor *)color {
	CGRect r = CGRectMake(0, 0, 1, 1);
	UIGraphicsBeginImageContextWithOptions(r.size, NO, 0);
	[color setFill];
	UIRectFill(r);
	UIImage *img = UIGraphicsGetImageFromCurrentImageContext();
	UIGraphicsEndImageContext();
	return [img resizableImageWithCapInsets:UIEdgeInsetsZero];
}

- (void)accessoryTapped:(UIButton *)sender {
	[self noteInput];
	int action = (int)sender.tag;
	if (action == kAccessoryDone) {
		// Commit preedit into the field, then drop focus so the next frame does
		// not re-becomeFirstResponder via WantsKeyboard.
		[self commitMarked];
	}
	shireiAccessoryAction(action);
}

- (UIKeyboardType)keyboardType {
	return UIKeyboardTypeDefault;
}
- (UIReturnKeyType)returnKeyType {
	return UIReturnKeyDefault;
}
- (UITextAutocorrectionType)autocorrectionType {
	return UITextAutocorrectionTypeNo;
}
- (UITextAutocapitalizationType)autocapitalizationType {
	return UITextAutocapitalizationTypeNone;
}
- (UITextSpellCheckingType)spellCheckingType {
	return UITextSpellCheckingTypeNo;
}

// ---- UIKeyInput ----

- (BOOL)hasText {
	// App buffer is in Go; keep backspace enabled while first responder.
	// While composing, _doc may also be non-empty.
	return YES;
}

- (void)insertText:(NSString *)text {
	if (text.length == 0) {
		return;
	}
	// Committing ends the marked session (IME confirm path).
	[self.inputDelegate textWillChange:self];
	[self.inputDelegate selectionWillChange:self];
	if ([self hasMarkedText]) {
		[self clearMarkedLocal];
		shireiClearComposition();
	}
	const char *utf8 = text.UTF8String;
	if (utf8) {
		shireiInsertText((char *)utf8);
	}
	_selStart = 0;
	_selEnd = 0;
	[self.inputDelegate selectionDidChange:self];
	[self.inputDelegate textDidChange:self];
	[self noteInput];
}

- (void)deleteBackward {
	[self noteInput];
	if ([self hasMarkedText]) {
		// Prefer shrinking marked text locally; do not delete from the Go buffer.
		[self.inputDelegate textWillChange:self];
		[self.inputDelegate selectionWillChange:self];
		if (_doc.length <= 1) {
			[self clearMarkedLocal];
			shireiClearComposition();
		} else {
			// Delete one composed character (one UTF-16 code unit; surrogate pairs
			// may need two — rangeOfComposedCharacterSequence handles that).
			NSRange last = [_doc rangeOfComposedCharacterSequenceAtIndex:_doc.length - 1];
			[_doc deleteCharactersInRange:last];
			_markedRange = NSMakeRange(0, _doc.length);
			_selStart = _doc.length;
			_selEnd = _doc.length;
			[self notifyCompositionFromDoc];
		}
		[self.inputDelegate selectionDidChange:self];
		[self.inputDelegate textDidChange:self];
		return;
	}
	shireiDeleteBackward();
}

// ---- UITextInput ----

- (id<UITextInputTokenizer>)tokenizer {
	return _tokenizer;
}

- (UITextPosition *)beginningOfDocument {
	return [ShireiTextPosition positionWithIndex:0];
}

- (UITextPosition *)endOfDocument {
	return [ShireiTextPosition positionWithIndex:_doc.length];
}

- (UITextRange *)selectedTextRange {
	// Japanese IMEs may query selection before starting marked text. An empty
	// range at 0 (not nil) matches Cocoa's selectedRange={0,0} lesson.
	return [ShireiTextRange rangeWithStart:_selStart end:_selEnd];
}

- (void)setSelectedTextRange:(UITextRange *)range {
	if (!range) {
		return;
	}
	ShireiTextRange *r = (ShireiTextRange *)range;
	NSUInteger a = MIN(r.startIndex, r.endIndex);
	NSUInteger b = MAX(r.startIndex, r.endIndex);
	a = MIN(a, _doc.length);
	b = MIN(b, _doc.length);
	[self.inputDelegate selectionWillChange:self];
	_selStart = a;
	_selEnd = b;
	[self.inputDelegate selectionDidChange:self];
	[self noteInput];
}

- (UITextRange *)markedTextRange {
	if (![self hasMarkedText]) {
		return nil;
	}
	return [ShireiTextRange rangeWithStart:_markedRange.location
	                                   end:_markedRange.location + _markedRange.length];
}

- (void)setMarkedText:(NSString *)markedText selectedRange:(NSRange)selectedRange {
	NSString *s = [self plainString:markedText];
	[self.inputDelegate textWillChange:self];
	[self.inputDelegate selectionWillChange:self];
	if (s.length == 0) {
		[self clearMarkedLocal];
		shireiClearComposition();
	} else {
		[_doc setString:s];
		_markedRange = NSMakeRange(0, s.length);
		NSRange sel = [self clampedSelection:selectedRange forLength:s.length];
		_selStart = sel.location;
		_selEnd = sel.location + sel.length;
		[self notifyCompositionFromDoc];
	}
	[self.inputDelegate selectionDidChange:self];
	[self.inputDelegate textDidChange:self];
	[self noteInput];
}

- (void)unmarkText {
	// End marked session by committing into the app (Cocoa unmarkText path).
	[self commitMarked];
}

- (NSString *)textInRange:(UITextRange *)range {
	if (!range) {
		return nil;
	}
	NSUInteger a = MIN(((ShireiTextRange *)range).startIndex, ((ShireiTextRange *)range).endIndex);
	NSUInteger b = MAX(((ShireiTextRange *)range).startIndex, ((ShireiTextRange *)range).endIndex);
	a = MIN(a, _doc.length);
	b = MIN(b, _doc.length);
	if (a >= b) {
		return @"";
	}
	return [_doc substringWithRange:NSMakeRange(a, b - a)];
}

- (void)replaceRange:(UITextRange *)range withText:(NSString *)text {
	// Thin document: treat as insertText of the replacement (common IME path).
	if (text.length == 0 && [self hasMarkedText]) {
		[self.inputDelegate textWillChange:self];
		[self clearMarkedLocal];
		shireiClearComposition();
		[self.inputDelegate textDidChange:self];
		[self noteInput];
		return;
	}
	if (text.length > 0) {
		[self insertText:text];
	}
}

- (UITextRange *)textRangeFromPosition:(UITextPosition *)fromPosition
                            toPosition:(UITextPosition *)toPosition {
	NSUInteger a = ((ShireiTextPosition *)fromPosition).index;
	NSUInteger b = ((ShireiTextPosition *)toPosition).index;
	return [ShireiTextRange rangeWithStart:a end:b];
}

- (UITextPosition *)positionFromPosition:(UITextPosition *)position offset:(NSInteger)offset {
	NSInteger i = (NSInteger)((ShireiTextPosition *)position).index + offset;
	if (i < 0) {
		i = 0;
	}
	if (i > (NSInteger)_doc.length) {
		i = (NSInteger)_doc.length;
	}
	return [ShireiTextPosition positionWithIndex:(NSUInteger)i];
}

- (UITextPosition *)positionFromPosition:(UITextPosition *)position
                             inDirection:(UITextLayoutDirection)direction
                                  offset:(NSInteger)offset {
	NSInteger delta = offset;
	if (direction == UITextLayoutDirectionLeft || direction == UITextLayoutDirectionUp) {
		delta = -offset;
	}
	return [self positionFromPosition:position offset:delta];
}

- (NSComparisonResult)comparePosition:(UITextPosition *)position
                           toPosition:(UITextPosition *)other {
	NSUInteger a = ((ShireiTextPosition *)position).index;
	NSUInteger b = ((ShireiTextPosition *)other).index;
	if (a < b) {
		return NSOrderedAscending;
	}
	if (a > b) {
		return NSOrderedDescending;
	}
	return NSOrderedSame;
}

- (NSInteger)offsetFromPosition:(UITextPosition *)from
                     toPosition:(UITextPosition *)toPosition {
	return (NSInteger)((ShireiTextPosition *)toPosition).index -
	       (NSInteger)((ShireiTextPosition *)from).index;
}

- (UITextPosition *)positionWithinRange:(UITextRange *)range
                    farthestInDirection:(UITextLayoutDirection)direction {
	ShireiTextRange *r = (ShireiTextRange *)range;
	if (direction == UITextLayoutDirectionRight || direction == UITextLayoutDirectionDown) {
		return r.end;
	}
	return r.start;
}

- (UITextRange *)characterRangeByExtendingPosition:(UITextPosition *)position
                                       inDirection:(UITextLayoutDirection)direction {
	NSUInteger i = ((ShireiTextPosition *)position).index;
	if (direction == UITextLayoutDirectionRight || direction == UITextLayoutDirectionDown) {
		NSUInteger j = MIN(i + 1, _doc.length);
		return [ShireiTextRange rangeWithStart:i end:j];
	}
	NSUInteger j = (i > 0) ? i - 1 : 0;
	return [ShireiTextRange rangeWithStart:j end:i];
}

- (UITextWritingDirection)baseWritingDirectionForPosition:(UITextPosition *)position
                                              inDirection:(UITextStorageDirection)direction {
	return UITextWritingDirectionNatural;
}

- (void)setBaseWritingDirection:(UITextWritingDirection)writingDirection
                       forRange:(UITextRange *)range {
	(void)writingDirection;
	(void)range;
}

// Caret / marked geometry from last shirei frame (logical points in this view).
- (CGRect)caretRectInView {
	CGFloat x = (CGFloat)shireiCaretX();
	CGFloat y = (CGFloat)shireiCaretY(); // bottom of caret
	CGFloat h = (CGFloat)shireiCaretHeight();
	if (h <= 0) {
		h = 16;
	}
	return CGRectMake(x, y - h, 1, h);
}

- (CGRect)firstRectForRange:(UITextRange *)range {
	(void)range;
	return [self caretRectInView];
}

- (CGRect)caretRectForPosition:(UITextPosition *)position {
	(void)position;
	return [self caretRectInView];
}

- (NSArray<UITextSelectionRect *> *)selectionRectsForRange:(UITextRange *)range {
	(void)range;
	return @[];
}

- (UITextPosition *)closestPositionToPoint:(CGPoint)point {
	(void)point;
	return [ShireiTextPosition positionWithIndex:_selStart];
}

- (UITextPosition *)closestPositionToPoint:(CGPoint)point withinRange:(UITextRange *)range {
	(void)point;
	return range.start;
}

- (UITextRange *)characterRangeAtPoint:(CGPoint)point {
	(void)point;
	return [ShireiTextRange rangeWithStart:_selStart end:_selEnd];
}

// Optional dictation stubs — silence unrecognized selector noise.
- (void)dictationRecordingDidEnd {
}
- (void)dictationRecognitionFailed {
}

- (void)keyboardFrameChanged:(NSNotification *)note {
	NSDictionary *info = note.userInfo;
	NSValue *val = info[UIKeyboardFrameEndUserInfoKey];
	if (val) {
		gKeyboardFrameScreen = [val CGRectValue];
	} else {
		gKeyboardFrameScreen = CGRectNull;
	}
	ios_layoutContentView();
}

- (void)shireiTick:(CADisplayLink *)link {
	(void)link;
	BOOL recent = (CFAbsoluteTimeGetCurrent() - gLastInputTime) < kInputRenderWindow;
	if (!gWantsFrame && !recent && !shireiFrameRequested()) {
		return;
	}
	// Hold paint only until aspect matches PreferredOrientation (portrait flash).
	// Prefer packaging orientation-landscape so the OS does not force-rotate.
	if (!ios_orientationGeometryReady()) {
		ios_applyOrientation(YES);
		gWantsFrame = YES;
		return;
	}
	double w = 0, h = 0;
	ios_viewSize(&w, &h);
	if (w <= 0 || h <= 0) {
		return;
	}
	shireiProduceAndPresent(w, h);
}
@end

void ios_attach(void) {
	if (gWindow) {
		return;
	}

	gKeyboardFrameScreen = CGRectNull;

	UIScreen *screen = [UIScreen mainScreen];
	gWindow = [[UIWindow alloc] initWithFrame:screen.bounds];
	gWindow.backgroundColor = [UIColor blackColor];

	ShireiRootVC *vc = [[ShireiRootVC alloc] init];
	gWindow.rootViewController = vc;

	gView = [[ShireiView alloc] initWithFrame:vc.view.bounds];
	// Frame owned by ios_layoutContentView (safe area + keyboard); not autoresized.
	gView.autoresizingMask = UIViewAutoresizingNone;
	[vc.view addSubview:gView];

	// Request preferred geometry before the window is shown when possible.
	// (Full scene geometry update often needs makeKeyAndVisible first.)
	ios_applyOrientation(YES);

	[gWindow makeKeyAndVisible];

	// Scene is available after key/visible — push landscape/portrait again.
	ios_applyOrientation(YES);

	// Keyboard end frame includes accessory views when present — same path for a
	// future inputAccessoryView toolbar.
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	[nc addObserver:gView
	       selector:@selector(keyboardFrameChanged:)
	           name:UIKeyboardWillChangeFrameNotification
	         object:nil];
	[nc addObserver:gView
	       selector:@selector(keyboardFrameChanged:)
	           name:UIKeyboardDidChangeFrameNotification
	         object:nil];

	// Hardware keyboard attach/detach (Bluetooth Magic Keyboard, Smart Keyboard, …).
	if (@available(iOS 14.0, *)) {
		void (^onKB)(NSNotification *) = ^(NSNotification *note) {
			(void)note;
			ios_refreshHardwareKeyboard();
		};
		[nc addObserverForName:GCKeyboardDidConnectNotification
		                object:nil
		                 queue:[NSOperationQueue mainQueue]
		            usingBlock:onKB];
		[nc addObserverForName:GCKeyboardDidDisconnectNotification
		                object:nil
		                 queue:[NSOperationQueue mainQueue]
		            usingBlock:onKB];
	}
	ios_refreshHardwareKeyboard();

	if (vc.isViewLoaded) {
		[vc.view setNeedsLayout];
		[vc.view layoutIfNeeded];
	}
	ios_layoutContentView();

	gLastInputTime = CFAbsoluteTimeGetCurrent();
	gWantsFrame = YES;
	gLink = [CADisplayLink displayLinkWithTarget:gView selector:@selector(shireiTick:)];
	[gLink addToRunLoop:[NSRunLoop mainRunLoop] forMode:NSRunLoopCommonModes];
}

void ios_setWantsFrame(int v) {
	gWantsFrame = v ? YES : NO;
}

void ios_syncKeyboard(int wants) {
	if (!gView) {
		return;
	}
	if (wants) {
		if (![gView isFirstResponder]) {
			[gView becomeFirstResponder];
		}
	} else {
		if ([gView isFirstResponder]) {
			// resignFirstResponder commits marked via our override.
			[gView resignFirstResponder];
		}
	}
}

double ios_screenScale(void) {
	UIScreen *s = [UIScreen mainScreen];
	return s ? (double)s.scale : 1.0;
}

void ios_viewSize(double *outW, double *outH) {
	if (!outW || !outH) {
		return;
	}
	if (gLayoutW > 0 && gLayoutH > 0) {
		*outW = (double)gLayoutW;
		*outH = (double)gLayoutH;
		return;
	}
	if (gView) {
		CGSize sz = gView.bounds.size;
		*outW = (double)sz.width;
		*outH = (double)sz.height;
		return;
	}
	CGRect b = [UIScreen mainScreen].bounds;
	*outW = (double)b.size.width;
	*outH = (double)b.size.height;
}

void ios_drawableSize(double *outW, double *outH) {
	if (!outW || !outH) {
		return;
	}
	if (gView) {
		CGSize sz = gView.bounds.size;
		*outW = (double)sz.width;
		*outH = (double)sz.height;
		return;
	}
	ios_viewSize(outW, outH);
}

void ios_setClipboard(const char *s) {
	if (!s) {
		return;
	}
	[UIPasteboard generalPasteboard].string = [NSString stringWithUTF8String:s];
}

char *ios_getClipboard(void) {
	NSString *s = [UIPasteboard generalPasteboard].string;
	if (!s || s.length == 0) {
		return NULL;
	}
	const char *utf8 = s.UTF8String;
	if (!utf8) {
		return NULL;
	}
	return strdup(utf8);
}

void ios_openURL(const char *url) {
	if (!url) {
		return;
	}
	NSString *s = [NSString stringWithUTF8String:url];
	if (!s || s.length == 0) {
		return;
	}
	NSURL *u = [NSURL URLWithString:s];
	if (!u) {
		return;
	}
	// UIApplication must be messaged on the main thread.
	dispatch_async(dispatch_get_main_queue(), ^{
		UIApplication *app = [UIApplication sharedApplication];
		if (!app) {
			return;
		}
		[app openURL:u options:@{} completionHandler:nil];
	});
}

void *ios_rootViewController(void) {
	if (!gWindow) {
		return NULL;
	}
	return (__bridge void *)gWindow.rootViewController;
}

void ios_present_bgra(const void *pixels, int stride, int width, int height) {
	if (!gView || !pixels || width <= 0 || height <= 0 || stride < width * 4) {
		return;
	}

	size_t nbytes = (size_t)stride * (size_t)height;
	void *copy = malloc(nbytes);
	if (!copy) {
		return;
	}
	memcpy(copy, pixels, nbytes);

	CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
	CGContextRef ctx = CGBitmapContextCreate(
		copy, (size_t)width, (size_t)height, 8, (size_t)stride, cs,
		kCGBitmapByteOrder32Little | kCGImageAlphaPremultipliedFirst);
	CGColorSpaceRelease(cs);
	if (!ctx) {
		free(copy);
		return;
	}
	CGImageRef img = CGBitmapContextCreateImage(ctx);
	CGContextRelease(ctx);
	free(copy);
	if (!img) {
		return;
	}

	gView.layer.contents = (__bridge id)img;
	CGImageRelease(img);
}
