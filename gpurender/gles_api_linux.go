//go:build linux && !android

package gpurender

import "fmt"

func loadGLESLibs() error {
	libEGL, err := dlopenFirst("libEGL.so.1", "libEGL.so")
	if err != nil {
		return fmt.Errorf("libEGL: %w", err)
	}
	libGLES, err := dlopenFirst("libGLESv2.so.2", "libGLESv2.so")
	if err != nil {
		return fmt.Errorf("libGLESv2: %w", err)
	}
	libGBM, err := dlopenFirst("libgbm.so.1", "libgbm.so")
	if err != nil {
		return fmt.Errorf("libgbm: %w", err)
	}
	if err := bindCommonGL(libEGL, libGLES); err != nil {
		return err
	}
	need := []struct {
		lib  uintptr
		name string
		fn   interface{}
	}{
		{libGBM, "gbm_create_device", &gbmCreateDevice},
		{libGBM, "gbm_device_destroy", &gbmDeviceDestroy},
		{libGBM, "gbm_bo_create", &gbmBoCreate},
		{libGBM, "gbm_bo_destroy", &gbmBoDestroy},
		{libGBM, "gbm_bo_get_fd", &gbmBoGetFd},
		{libGBM, "gbm_bo_get_stride", &gbmBoGetStride},
	}
	for _, s := range need {
		if err := bindSym(s.lib, s.name, s.fn); err != nil {
			return err
		}
	}
	bindOpt(libEGL, "eglGetPlatformDisplay", &eglGetPlatformDisplay)
	if !bindOpt(libEGL, "eglCreateImageKHR", &eglCreateImageKHR) {
		if err := bindSym(libEGL, "eglCreateImage", &eglCreateImage15); err != nil {
			return fmt.Errorf("eglCreateImage: %w", err)
		}
	}
	if !bindOpt(libEGL, "eglDestroyImageKHR", &eglDestroyImageKHR) {
		bindOpt(libEGL, "eglDestroyImage", &eglDestroyImageKHR)
	}
	haveBoOffset = bindOpt(libGBM, "gbm_bo_get_offset", &gbmBoGetOffset)
	haveBoModifier = bindOpt(libGBM, "gbm_bo_get_modifier", &gbmBoGetModifier)
	haveCreateWithModifiers = bindOpt(libGBM, "gbm_bo_create_with_modifiers", &gbmBoCreateWithModifiers)
	haveCreateWithModifiers2 = bindOpt(libGBM, "gbm_bo_create_with_modifiers2", &gbmBoCreateWithModifiers2)
	havePlaneCount = bindOpt(libGBM, "gbm_bo_get_plane_count", &gbmBoGetPlaneCount)
	haveFdForPlane = bindOpt(libGBM, "gbm_bo_get_fd_for_plane", &gbmBoGetFdForPlane)
	haveStrideForPlane = bindOpt(libGBM, "gbm_bo_get_stride_for_plane", &gbmBoGetStrideForPlane)
	haveFramebufferParameteri = bindOpt(libGLES, "glFramebufferParameteri", &glFramebufferParameteri)
	if err := bindSym(libGLES, "glEGLImageTargetTexture2DOES", &glEGLImageTargetTexture2DOES); err != nil {
		return err
	}
	return nil
}
