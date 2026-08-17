//go:build darwin && !ios && !x11darwin

#import <Cocoa/Cocoa.h>
#import "darkmode_darwin.h"

extern void shireiExtDarkmodeOnUpdate(int isDark);

static id sThemeObserver = nil;

int shirei_ext_darkmode_darwin_is_dark(void) {
    @autoreleasepool {
        if (@available(macOS 10.14, *)) {
            if (NSApp && [NSApp effectiveAppearance]) {
                NSAppearanceName match = [[NSApp effectiveAppearance] bestMatchFromAppearancesWithNames:@[
                    NSAppearanceNameAqua,
                    NSAppearanceNameDarkAqua
                ]];
                if (match) {
                    return [match isEqualToString:NSAppearanceNameDarkAqua] ? 1 : 0;
                }
            }
            NSString *style = [[NSUserDefaults standardUserDefaults] stringForKey:@"AppleInterfaceStyle"];
            if (style && [style isEqualToString:@"Dark"]) {
                return 1;
            }
        }
        return 0;
    }
}

void shirei_ext_darkmode_darwin_start_observer(void) {
    @autoreleasepool {
        if (@available(macOS 10.14, *)) {
            if (!sThemeObserver) {
                sThemeObserver = [[NSDistributedNotificationCenter defaultCenter]
                    addObserverForName:@"AppleInterfaceThemeChangedNotification"
                                object:nil
                                 queue:[NSOperationQueue mainQueue]
                            usingBlock:^(NSNotification * _Nonnull note) {
                                shireiExtDarkmodeOnUpdate(shirei_ext_darkmode_darwin_is_dark());
                            }];
            }
        }
    }
}
