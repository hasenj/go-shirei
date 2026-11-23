//go:build android

package main

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/androidbackend"
)

const (
	readMediaVideoPerm      = "android.permission.READ_MEDIA_VIDEO"
	readExternalStoragePerm = "android.permission.READ_EXTERNAL_STORAGE"
	videoPermReqCode        = 4101
)

var videoPermAsked bool

// ensureVideoPermission prompts once for video storage permissions on Android
// before running onReady so shared folders (/sdcard/Movies, /sdcard/DCIM) are
// readable in the file browser. Requests both READ_MEDIA_VIDEO (API 33+) and
// READ_EXTERNAL_STORAGE (targetSdk <= 32). If already granted or already asked
// this session, onReady runs immediately.
func ensureVideoPermission(onReady func()) {
	hasPerm := androidbackend.CheckPermission(readMediaVideoPerm) ||
		androidbackend.CheckPermission(readExternalStoragePerm)
	if hasPerm || videoPermAsked {
		onReady()
		return
	}
	videoPermAsked = true
	androidbackend.OnPermissionResult(videoPermReqCode, func(_ []int) {
		shirei.Post(onReady)
	})
	androidbackend.RequestPermissions([]string{readMediaVideoPerm, readExternalStoragePerm}, videoPermReqCode)
}
