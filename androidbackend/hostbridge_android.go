//go:build android

// Generic host bridges for Android extensions: activity results, permissions,
// and access to the live Activity. Feature packages (e.g. camera) use these
// instead of forking ShireiActivity.
package androidbackend

/*
#include "shim.h"
#include <stdlib.h>
*/
import "C"

import (
	"sync"
	"unsafe"
)

// ActivityResultFunc handles onActivityResult.
// env and intent are JNI pointers valid only for the duration of the call
// (UI thread). resultCode is Activity.RESULT_* (−1 = OK, 0 = CANCELED).
type ActivityResultFunc func(env unsafe.Pointer, resultCode int, intent unsafe.Pointer)

// PermissionResultFunc handles onRequestPermissionsResult.
// grantResults[i] == 0 means PackageManager.PERMISSION_GRANTED.
type PermissionResultFunc func(grantResults []int)

var (
	hostMu       sync.Mutex
	actHandlers  = map[int]ActivityResultFunc{}
	permHandlers = map[int]PermissionResultFunc{}
)

// OnActivityResult registers fn for requestCode (replaces any previous).
// Call before StartActivityForResult.
func OnActivityResult(requestCode int, fn ActivityResultFunc) {
	hostMu.Lock()
	if fn == nil {
		delete(actHandlers, requestCode)
	} else {
		actHandlers[requestCode] = fn
	}
	hostMu.Unlock()
}

// OnPermissionResult registers fn for requestCode from RequestPermissions.
func OnPermissionResult(requestCode int, fn PermissionResultFunc) {
	hostMu.Lock()
	if fn == nil {
		delete(permHandlers, requestCode)
	} else {
		permHandlers[requestCode] = fn
	}
	hostMu.Unlock()
}

// StartActivityForResult starts intent (jobject) for result. App-thread safe.
func StartActivityForResult(intent unsafe.Pointer, requestCode int) {
	if intent == nil {
		return
	}
	C.shirei_android_start_activity_for_result(intent, C.int(requestCode))
}

// RequestPermissions requests the named permissions (e.g. "android.permission.CAMERA").
func RequestPermissions(perms []string, requestCode int) {
	if len(perms) == 0 {
		return
	}
	cperms := make([]*C.char, len(perms)+1)
	for i, p := range perms {
		cperms[i] = C.CString(p)
	}
	defer func() {
		for i := range perms {
			C.free(unsafe.Pointer(cperms[i]))
		}
	}()
	C.shirei_android_request_permissions(&cperms[0], C.int(requestCode))
}

// CheckPermission reports whether perm is granted (true on pre-M).
func CheckPermission(perm string) bool {
	cs := C.CString(perm)
	defer C.free(unsafe.Pointer(cs))
	return C.shirei_android_check_permission(cs) == 0
}

// JNIEnv returns the app-thread JNIEnv*, or nil. Prefer the env passed into
// ActivityResultFunc when handling results (that runs on the UI thread).
func JNIEnv() unsafe.Pointer {
	return unsafe.Pointer(C.shirei_android_jni_env())
}

//export shireiAndroidActivityResult
func shireiAndroidActivityResult(requestCode, resultCode C.int, env, intent unsafe.Pointer) {
	hostMu.Lock()
	fn := actHandlers[int(requestCode)]
	hostMu.Unlock()
	if fn != nil {
		fn(env, int(resultCode), intent)
	}
}

//export shireiAndroidPermissionResult
func shireiAndroidPermissionResult(requestCode C.int, grants *C.int, n C.int) {
	var slice []int
	if grants != nil && n > 0 {
		tmp := unsafe.Slice((*int32)(unsafe.Pointer(grants)), int(n))
		slice = make([]int, len(tmp))
		for i, v := range tmp {
			slice[i] = int(v)
		}
	}
	hostMu.Lock()
	fn := permHandlers[int(requestCode)]
	hostMu.Unlock()
	if fn != nil {
		fn(slice)
	}
}
