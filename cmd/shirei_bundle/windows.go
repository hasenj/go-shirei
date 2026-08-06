package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var windowsBundleSteps = []string{
	"Build binary",
	"Assemble package",
	"Create zip",
}

// WindowsBundleOpts is one Windows release packaging job (one or more arch zips).
type WindowsBundleOpts struct {
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

func (o WindowsBundleOpts) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// bundleWindows builds Windows binaries and packages each arch as product/ + .exe (+ icon) in a .zip.
func bundleWindows(o WindowsBundleOpts) (primary string, extra []string, err error) {
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

	buildDir, err := desktopCacheBuildDir(product + "-windows")
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

	if err := step(); err != nil {
		return "", nil, err
	}
	binByArch := map[string]string{}
	for _, arch := range archs {
		binPath := filepath.Join(buildDir, product+"-"+arch+".exe")
		if err := buildDesktopBinary(moduleRoot, pkgSpec, binPath, "windows", arch,
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			return "", nil, err
		}
		binByArch[arch] = binPath
	}

	if err := step(); err != nil {
		return "", nil, err
	}
	destRoot, err := resolvePlatformReleaseDir(o.ReleaseDir, product, version, platformWindows)
	if err != nil {
		return "", nil, err
	}

	var archives []string
	for _, arch := range archs {
		stage := filepath.Join(buildDir, product+"-windows-"+arch)
		_ = os.RemoveAll(stage)
		if err := os.MkdirAll(stage, 0o755); err != nil {
			return "", nil, err
		}
		destExe := filepath.Join(stage, product+".exe")
		if err := copyFile(binByArch[arch], destExe); err != nil {
			return "", nil, fmt.Errorf("copy binary: %w", err)
		}
		// Packaged app assets (<package>/Resources → stage/Resources)
		stageRes := filepath.Join(stage, packageResourcesDir)
		if err := copyPackageResources(o.PkgDir, stageRes, o.logf); err != nil {
			return "", nil, err
		}
		if _, err := copyIconIfPresent(o.IconPath, stage, product); err != nil {
			return "", nil, fmt.Errorf("copy icon: %w", err)
		}
		_ = os.WriteFile(filepath.Join(stage, "APP_ID.txt"), []byte(o.BundleID+"\n"), 0o644)

		if len(archives) == 0 {
			if err := step(); err != nil {
				return "", nil, err
			}
		}
		zipName := fmt.Sprintf("%s-%s-%s-windows-%s.zip", product, version, build, arch)
		zipPath := filepath.Join(destRoot, zipName)
		o.logf("— zip %s", zipPath)
		if err := zipDir(stage, zipPath); err != nil {
			return "", nil, fmt.Errorf("zip: %w", err)
		}
		archives = append(archives, zipPath)
		o.logf("windows archive: %s", zipPath)
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
