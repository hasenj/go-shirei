//go:build ios

#import <UIKit/UIKit.h>
#import "darkmode_ios.h"

extern void shireiExtDarkmodeIOSUpdate(int isDark);

int shirei_ext_darkmode_ios_is_dark(void) {
    @autoreleasepool {
        if (@available(iOS 13.0, *)) {
            UIUserInterfaceStyle style = [UITraitCollection currentTraitCollection].userInterfaceStyle;
            if (style == UIUserInterfaceStyleDark) {
                return 1;
            }
            if (style == UIUserInterfaceStyleLight) {
                return 0;
            }
            if ([UIScreen mainScreen].traitCollection.userInterfaceStyle == UIUserInterfaceStyleDark) {
                return 1;
            }
        }
        return 0;
    }
}

void shirei_ext_darkmode_ios_start_observer(void) {
    // iOS UI trait collection observations are attached to view hierarchies.
    // The initial query is evaluated at initPlatform.
}
