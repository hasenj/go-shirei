// Package xcursor loads and parses the X cursor
package xcursor

import (
	"bufio"
	"io"
	"os"
	"path"
	"strings"
)

// CursorTheme represents an Xcursor theme
type CursorTheme struct {
	theme       *cursorTheme
	SearchPaths []string
}

// Load loads an Xcursor theme by name
func Load(name string) *CursorTheme {
	searchPaths := themeSearchPaths()
	theme := load(name, searchPaths)

	return &CursorTheme{
		SearchPaths: searchPaths,
		theme:       theme,
	}
}

// LoadIcon loads an Xcursor icon path by icon name from theme
func (c *CursorTheme) LoadIcon(iconName string) string {
	return c.theme.loadIcon(iconName, c.SearchPaths, map[string]struct{}{})
}

type cursorTheme struct {
	name  string
	paths []string
}

func load(name string, searchPaths []string) *cursorTheme {
	var paths []string

	for _, p := range searchPaths {
		p = path.Join(p, name)

		file, err := os.Stat(p)
		if err == nil && file.IsDir() {
			pathDir := p

			p = path.Join(p, "index.theme")

			inherits := themeInherits(p)
			if inherits == "" {
				if name != "default" {
					inherits = "default"
				} else {
					continue
				}
			}

			paths = append(paths, path.Join(pathDir, inherits))
		}
	}

	return &cursorTheme{
		name:  name,
		paths: paths,
	}
}

func (c *cursorTheme) loadIcon(
	iconName string,
	searchPaths []string,
	walkedThemes map[string]struct{},
) string {
	for _, p := range c.paths {
		pdir, _ := path.Split(p)
		iconPath := path.Join(pdir, "cursors", iconName)

		file, err := os.Stat(iconPath)
		if err == nil && !file.IsDir() {
			return iconPath
		}
	}

	walkedThemes[c.name] = struct{}{}

	for _, p := range c.paths {
		_, inherits := path.Split(p)
		if inherits == "" {
			continue
		}

		if _, ok := walkedThemes[inherits]; ok {
			continue
		}

		inheritedTheme := load(inherits, searchPaths)

		iconPath := inheritedTheme.loadIcon(iconName, searchPaths, walkedThemes)
		if iconPath != "" {
			return iconPath
		}
	}

	return ""
}

func themeSearchPaths() []string {
	var xcursorPaths []string

	xcursorPathEnv := os.Getenv("XCURSOR_PATH")
	if xcursorPathEnv == "" {
		getIconDirs := func(xdgPath string) []string {
			paths := strings.Split(xdgPath, ":")
			for i, v := range paths {
				paths[i] = path.Join(v, "icons")
			}

			return paths
		}

		xdgDataHomeEnv := os.Getenv("XDG_DATA_HOME")
		if xdgDataHomeEnv == "" {
			xdgDataHomeEnv = "~/.local/share"
		}

		xdgDataHome := getIconDirs(xdgDataHomeEnv)

		xdgDataDirsEnv := os.Getenv("XDG_DATA_DIRS")
		if xdgDataDirsEnv == "" {
			xdgDataDirsEnv = "/usr/local/share:/usr/share"
		}

		xdgDataDirs := getIconDirs(xdgDataDirsEnv)

		xcursorPaths = append(xcursorPaths, xdgDataHome...)
		xcursorPaths = append(xcursorPaths, "~/.icons")
		xcursorPaths = append(xcursorPaths, xdgDataDirs...)
		xcursorPaths = append(
			xcursorPaths,
			"/usr/share/pixmaps",
			"~/.cursors",
			"/usr/share/cursors/xorg-x11",
		)
	} else {
		xcursorPaths = strings.Split(xcursorPathEnv, ":")
	}

	homeDir := os.Getenv("HOME")
	for i, v := range xcursorPaths {
		xcursorPaths[i] = strings.Replace(v, "~", homeDir, 1)
	}

	return xcursorPaths
}

func themeInherits(filePath string) string {
	content, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer content.Close()

	return parseTheme(content)
}

func parseTheme(content io.Reader) string {
	const pattern = "Inherits"

	sc := bufio.NewScanner(content)
	for sc.Scan() {
		line := sc.Text()

		if !strings.HasPrefix(line, pattern) {
			continue
		}

		chars := strings.TrimPrefix(line, pattern)
		chars = strings.TrimSpace(chars)

		if !strings.HasPrefix(chars, "=") {
			continue
		}

		result := strings.NewReplacer(
			"\t", "", "\n", "", "\v", "", "\f", "", "\r", "", " ", "", "\u0085", "", "\u00A0", "",
			";", "",
			",", "",
			"=", "",
		).Replace(chars)

		if result != "" {
			return result
		}
	}

	return ""
}
