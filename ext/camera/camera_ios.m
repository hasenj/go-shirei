// UIImagePickerController bridge for go.hasen.dev/shirei/ext/camera.
#import <UIKit/UIKit.h>
#include <stdlib.h>
#include <string.h>
#include "camera_ios.h"
#include "_cgo_export.h"

@interface ShireiCameraDelegate : NSObject <UIImagePickerControllerDelegate, UINavigationControllerDelegate>
@end

@implementation ShireiCameraDelegate

static void deliver_fail(int status) {
	cameraIOSOnResult(NULL, 0, 0, 0, status);
}

- (void)imagePickerControllerDidCancel:(UIImagePickerController *)picker {
	[picker dismissViewControllerAnimated:YES completion:^{
		deliver_fail(1); // cancelled
	}];
}

- (void)imagePickerController:(UIImagePickerController *)picker
	didFinishPickingMediaWithInfo:(NSDictionary<UIImagePickerControllerInfoKey, id> *)info {
	UIImage *img = info[UIImagePickerControllerOriginalImage];
	if (!img) {
		[picker dismissViewControllerAnimated:YES completion:^{
			deliver_fail(2);
		}];
		return;
	}
	// Normalize orientation into an upright bitmap.
	UIGraphicsBeginImageContextWithOptions(img.size, YES, 1.0);
	[img drawInRect:CGRectMake(0, 0, img.size.width, img.size.height)];
	UIImage *flat = UIGraphicsGetImageFromCurrentImageContext();
	UIGraphicsEndImageContext();
	if (!flat) {
		flat = img;
	}

	CGImageRef cg = flat.CGImage;
	if (!cg) {
		[picker dismissViewControllerAnimated:YES completion:^{
			deliver_fail(2);
		}];
		return;
	}

	size_t w = CGImageGetWidth(cg);
	size_t h = CGImageGetHeight(cg);
	size_t stride = w * 4;
	uint8_t *buf = (uint8_t *)calloc(1, stride * h);
	if (!buf) {
		[picker dismissViewControllerAnimated:YES completion:^{
			deliver_fail(2);
		}];
		return;
	}

	CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
	CGContextRef ctx = CGBitmapContextCreate(
		buf, w, h, 8, stride, cs,
		kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
	CGColorSpaceRelease(cs);
	if (!ctx) {
		free(buf);
		[picker dismissViewControllerAnimated:YES completion:^{
			deliver_fail(2);
		}];
		return;
	}
	CGContextDrawImage(ctx, CGRectMake(0, 0, (CGFloat)w, (CGFloat)h), cg);
	CGContextRelease(ctx);

	// Premultiplied RGBA (big-endian byte order in memory: R,G,B,A) matches
	// Go image.RGBA layout on little-endian after this packing.
	// Actually kCGImageAlphaPremultipliedLast | 32Big stores R,G,B,A in
	// address order — same as image.RGBA.Pix. Good.

	[picker dismissViewControllerAnimated:YES completion:^{
		cameraIOSOnResult(buf, (int)w, (int)h, (int)stride, 0);
		free(buf);
	}];
}

@end

static ShireiCameraDelegate *gDelegate;

void camera_ios_present(void *viewController) {
	UIViewController *vc = (__bridge UIViewController *)viewController;
	if (!vc) {
		deliver_fail(2);
		return;
	}

	void (^present)(void) = ^{
		if (!gDelegate) {
			gDelegate = [[ShireiCameraDelegate alloc] init];
		}
		UIImagePickerController *picker = [[UIImagePickerController alloc] init];
		// Prefer camera; Simulator (and some devices) have none — fall back to
		// the photo library so the plugin path is still exercisable.
		if ([UIImagePickerController isSourceTypeAvailable:UIImagePickerControllerSourceTypeCamera]) {
			picker.sourceType = UIImagePickerControllerSourceTypeCamera;
		} else if ([UIImagePickerController isSourceTypeAvailable:UIImagePickerControllerSourceTypePhotoLibrary]) {
			picker.sourceType = UIImagePickerControllerSourceTypePhotoLibrary;
		} else {
			deliver_fail(2);
			return;
		}
		picker.delegate = gDelegate;
		picker.modalPresentationStyle = UIModalPresentationFullScreen;
		[vc presentViewController:picker animated:YES completion:nil];
	};

	if ([NSThread isMainThread]) {
		present();
	} else {
		dispatch_async(dispatch_get_main_queue(), present);
	}
}
