#import <Cocoa/Cocoa.h>
#include "window_darwin.h"

void window_setNSWindowMinSize(void *nswindow_ptr, double minW, double minH) {
    if (!nswindow_ptr) return;
    NSWindow *win = (__bridge NSWindow *)nswindow_ptr;
    void (^apply)(void) = ^{
        [win setContentMinSize:NSMakeSize(minW, minH)];
        NSRect contentRect = [win contentRectForFrameRect:[win frame]];
        if (contentRect.size.width < minW || contentRect.size.height < minH) {
            CGFloat w = fmax(contentRect.size.width, minW);
            CGFloat h = fmax(contentRect.size.height, minH);
            [win setContentSize:NSMakeSize(w, h)];
        }
    };
    if ([NSThread isMainThread]) {
        apply();
    } else {
        dispatch_async(dispatch_get_main_queue(), apply);
    }
}

void window_centerNSWindow(void *nswindow_ptr) {
    if (!nswindow_ptr) return;
    NSWindow *win = (__bridge NSWindow *)nswindow_ptr;
    void (^apply)(void) = ^{
        [win center];
    };
    if ([NSThread isMainThread]) {
        apply();
    } else {
        dispatch_async(dispatch_get_main_queue(), apply);
    }
}

void window_positionNSWindow(void *nswindow_ptr, int x, int y) {
    if (!nswindow_ptr) return;
    NSWindow *win = (__bridge NSWindow *)nswindow_ptr;
    void (^apply)(void) = ^{
        NSScreen *primary = [[NSScreen screens] firstObject];
        CGFloat screenH = primary ? primary.frame.size.height : 800;
        [win setFrameTopLeftPoint:NSMakePoint(x, screenH - y)];
    };
    if ([NSThread isMainThread]) {
        apply();
    } else {
        dispatch_async(dispatch_get_main_queue(), apply);
    }
}
