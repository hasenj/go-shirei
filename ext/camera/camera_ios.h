// iOS camera picker (experiment). Called from Go with a UIViewController*.
#ifndef SHIREI_CAMERA_IOS_H
#define SHIREI_CAMERA_IOS_H

#include <stdint.h>

// Present UIImagePickerController for still capture from vc (main thread).
// Result is delivered via cameraIOSOnResult (see camera_ios.go).
void camera_ios_present(void *viewController);

#endif
