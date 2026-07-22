// Minimal UIKit host for shirei iOS apps built as a Go c-archive.
// UIApplicationMain owns the process; after launch we call into Go, which
// runs the app's main() (SetupWindow + Run) and attaches the content view.
#import <UIKit/UIKit.h>

// Defined by the Go c-archive export (see ios-run.sh inject file).
void shirei_ios_run(void);

@interface ShireiAppDelegate : UIResponder <UIApplicationDelegate>
@property(nonatomic, strong) UIWindow *window;
@end

@implementation ShireiAppDelegate
- (BOOL)application:(UIApplication *)application
	didFinishLaunchingWithOptions:(NSDictionary *)launchOptions {
	(void)application;
	(void)launchOptions;
	// Initialize the Go runtime and run the app package's main (which calls
	// app.Run → iosbackend.Run → ios_attach). The window property is unused;
	// iosbackend creates its own UIWindow.
	shirei_ios_run();
	return YES;
}

// Defer to the key window's root VC so Host.PreferredOrientation (set before
// attach) controls the launch mask, not only the Info.plist union of all
// orientations.
- (UIInterfaceOrientationMask)application:(UIApplication *)application
	supportedInterfaceOrientationsForWindow:(UIWindow *)window {
	(void)application;
	UIViewController *root = window.rootViewController;
	if (root != nil) {
		return [root supportedInterfaceOrientations];
	}
	return UIInterfaceOrientationMaskAllButUpsideDown;
}
@end

int main(int argc, char *argv[]) {
	@autoreleasepool {
		return UIApplicationMain(argc, argv, nil,
			NSStringFromClass([ShireiAppDelegate class]));
	}
}
