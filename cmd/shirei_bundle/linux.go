package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var linuxBundleSteps = []string{
	"Build binary",
	"Assemble package",
	"Create tarball",
}

// LinuxBundleOpts is one Linux release packaging job (one or more arch tarballs).
type LinuxBundleOpts struct {
	PkgDir     string
	Name       string
	BundleID   string
	Version    string
	Build      string
	IconPath   string
	Archs      []string // arm64 and/or amd64
	ReleaseDir string
	Logf       func(format string, args ...any)
	OnStep     func(step int)
	Cancelled  func() bool
	SetRunningCmd func(cmd *exec.Cmd)
}

func (o LinuxBundleOpts) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// bundleLinux builds linux binaries and packages each arch as product/ + .desktop + icon in a .tar.gz.
// Returns primary tarball path and any additional arch archives.
func bundleLinux(o LinuxBundleOpts) (primary string, extra []string, err error) {
	if strings.TrimSpace(o.BundleID) == "" {
		return "", nil, fmt.Errorf("bundle id is required")
	}
	if st, err := os.Stat(o.PkgDir); err != nil || !st.IsDir() {
		return "", nil, fmt.Errorf("package dir not found: %s", o.PkgDir)
	}
	version := strings.TrimSpace(o.Version)
	build := strings.TrimSpace(o.Build)
	if version == "" || build == "" {
		return "", nil, fmt.Errorf("version and build are required")
	}
	archs := o.Archs
	if len(archs) == 0 {
		archs = []string{"amd64"}
	}

	display := strings.TrimSpace(o.Name)
	if display == "" {
		display = filepath.Base(o.PkgDir)
	}
	product := sanitizeProductName(display)
	if product == "" {
		product = "App"
	}

	moduleRoot, pkgSpec, err := resolvePackage(o.PkgDir)
	if err != nil {
		return "", nil, err
	}

	buildDir, err := desktopCacheBuildDir(product + "-linux")
	if err != nil {
		return "", nil, err
	}
	o.logf("build dir: %s", buildDir)
	o.logf("archs: %v", archs)

	stepN := 0
	step := func() error {
		if o.Cancelled != nil && o.Cancelled() {
			return fmt.Errorf("cancelled")
		}
		if o.OnStep != nil {
			o.OnStep(stepN)
		}
		stepN++
		return nil
	}

	// Step 0: build all arch binaries
	if err := step(); err != nil {
		return "", nil, err
	}
	binByArch := map[string]string{}
	for _, arch := range archs {
		binPath := filepath.Join(buildDir, product+"-"+arch)
		if err := buildDesktopBinary(moduleRoot, pkgSpec, binPath, "linux", arch,
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			return "", nil, err
		}
		binByArch[arch] = binPath
	}

	// Step 1: assemble per-arch directories
	if err := step(); err != nil {
		return "", nil, err
	}
	destRoot, err := resolvePlatformReleaseDir(o.ReleaseDir, product, version, platformLinux)
	if err != nil {
		return "", nil, err
	}

	var archives []string
	for _, arch := range archs {
		stage := filepath.Join(buildDir, product+"-linux-"+arch)
		_ = os.RemoveAll(stage)
		if err := os.MkdirAll(stage, 0o755); err != nil {
			return "", nil, err
		}
		// Binary
		destBin := filepath.Join(stage, product)
		if err := copyFile(binByArch[arch], destBin); err != nil {
			return "", nil, fmt.Errorf("copy binary: %w", err)
		}
		if err := os.Chmod(destBin, 0o755); err != nil {
			return "", nil, err
		}
		// Packaged app assets (<package>/Resources → stage/Resources)
		stageRes := filepath.Join(stage, packageResourcesDir)
		if err := copyPackageResources(o.PkgDir, stageRes, o.logf); err != nil {
			return "", nil, err
		}
		// Icon (optional)
		iconBase, err := copyIconIfPresent(o.IconPath, stage, product)
		if err != nil {
			return "", nil, fmt.Errorf("copy icon: %w", err)
		}
		iconName := product
		if iconBase != "" {
			// Desktop Icon= without extension is preferred when file is product.png
			iconName = strings.TrimSuffix(iconBase, filepath.Ext(iconBase))
		}
		// .desktop
		desktopPath := filepath.Join(stage, product+".desktop")
		comment := fmt.Sprintf("%s %s (build %s)", display, version, build)
		if err := writeLinuxDesktopFile(desktopPath, display, product, iconName, comment); err != nil {
			return "", nil, fmt.Errorf("desktop file: %w", err)
		}
		// Optional App ID note file for debugging (not required by Linux)
		_ = os.WriteFile(filepath.Join(stage, "APP_ID"), []byte(o.BundleID+"\n"), 0o644)

		// Step 2: tarball (progress step once for first archive; still log each)
		if len(archives) == 0 {
			if err := step(); err != nil {
				return "", nil, err
			}
		}
		tarName := fmt.Sprintf("%s-%s-%s-linux-%s.tar.gz", product, version, build, arch)
		tarPath := filepath.Join(destRoot, tarName)
		o.logf("— tar.gz %s", tarPath)
		if err := tarGzipDir(stage, tarPath); err != nil {
			return "", nil, fmt.Errorf("tarball: %w", err)
		}
		archives = append(archives, tarPath)
		o.logf("linux archive: %s", tarPath)
	}

	if len(archives) == 0 {
		return "", nil, fmt.Errorf("no archives produced")
	}
	primary = archives[0]
	if len(archives) > 1 {
		extra = archives[1:]
	}
	return primary, extra, nil
}
