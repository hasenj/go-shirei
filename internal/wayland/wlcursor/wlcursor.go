// Package wlcursor implements a Wayland cursor
package wlcursor

import (
	"fmt"
	"os"
	"strconv"

	sys "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"

	"go.hasen.dev/shirei/internal/wayland/wlcursor/xcursor"
)

// Image is a wlCursor cursor image
type Image interface {
	GetBuffer() *wl.Buffer
	GetWidth() int
	GetHeight() int
	GetHotspotX() int
	GetHotspotY() int
}

// interesting cursor icons.
const (
	BottomLeftCorner  = "bottom_left_corner"
	BottomRightCorner = "bottom_right_corner"
	BottomSide        = "bottom_side"
	Grabbing          = "grabbing"
	LeftPtr           = "left_ptr"
	LeftSide          = "left_side"
	RightSide         = "right_side"
	TopLeftCorner     = "top_left_corner"
	TopRightCorner    = "top_right_corner"
	TopSide           = "top_side"
	Xterm             = "xterm"
	Hand1             = "hand1"
	Watch             = "watch"
)

// Theme is a wlCursor cursor image theme
type Theme struct {
	Pool     *wl.ShmPool
	File     *os.File
	Name     string
	Cursors  []*Cursor
	Size     uint32
	PoolSize int32
}

// LoadTheme loads a default-named theme with default size and shm pool, based on environment
func LoadTheme(size uint32, shm *wl.Shm) (*Theme, error) {
	return LoadThemeOr("default", size, shm)
}

// LoadThemeOr loads a named theme with size and shm pool, based on environment
func LoadThemeOr(name string, size uint32, shm *wl.Shm) (*Theme, error) {
	themeName := os.Getenv("XCURSOR_THEME")
	if themeName == "" {
		themeName = name
	}

	var themeSize uint32
	themeSizeEnv := os.Getenv("XCURSOR_SIZE")

	themeSizeu64, err := strconv.ParseUint(themeSizeEnv, 10, 32)
	if err == nil {
		themeSize = uint32(themeSizeu64)
	} else {
		themeSize = size
	}

	return LoadThemeFromName(themeName, themeSize, shm)
}

// LoadThemeFromName loads a named theme with size and shm pool
func LoadThemeFromName(name string, size uint32, shm *wl.Shm) (*Theme, error) {
	const initialPoolSize = 16 * 16 * 4

	file, err := sys.CreateAnonymousFile(initialPoolSize)
	if err != nil {
		return nil, err
	}

	pool, err := shm.CreatePool(file.Fd(), initialPoolSize)
	if err != nil {
		return nil, err
	}

	return &Theme{
		Name:     name,
		Size:     size,
		Pool:     pool,
		PoolSize: initialPoolSize,
		File:     file,
	}, nil
}

// GetCursor gets a Theme cursor by name
func (t *Theme) GetCursor(name string) (*Cursor, error) {
	for _, cursor := range t.Cursors {
		if cursor.Name == name {
			return cursor, nil
		}
	}

	cursor, err := t.loadCursor(name, t.Size)
	if err != nil {
		return nil, err
	}

	t.Cursors = append(t.Cursors, cursor)

	return cursor, nil
}

func (t *Theme) loadCursor(name string, size uint32) (*Cursor, error) {
	iconPath := xcursor.Load(t.Name).LoadIcon(name)

	buf, err := os.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}

	images, err := xcursor.ParseXcursor(buf)
	if err != nil {
		return nil, err
	}

	return newCursor(name, t, images, size)
}

func (t *Theme) grow(size int32) error {
	if size > t.PoolSize {
		if err := t.File.Truncate(int64(size)); err != nil {
			return err
		}

		if err := t.Pool.Resize(size); err != nil {
			return err
		}

		t.PoolSize = size
	}

	return nil
}

// Destroy destroys a Theme
func (t *Theme) Destroy() (err error) {

	err = t.Pool.Destroy()
	if err != nil {
		t.File.Close()
		return fmt.Errorf("error when destroying theme: %w", err)
	}
	err = t.File.Close()
	return err
}

// Cursor is a Theme cursor
type Cursor struct {
	Name          string
	Images        []*ImageBuffer
	TotalDuration uint32
}

// GetCursorImage gets the n-th image from cursor or nil
func (c *Cursor) GetCursorImage(n int) *ImageBuffer {
	if n >= len(c.Images) {
		return nil
	}
	return c.Images[n]
}

func newCursor(name string, theme *Theme, images []*xcursor.Image, size uint32) (*Cursor, error) {
	totalDuration := uint32(0)

	nImages := nearestImages(size, images)

	imageBuffers := make([]*ImageBuffer, len(nImages))

	for i, image := range nImages {
		buffer, err := NewImageBuffer(theme, image)
		if err != nil {
			return nil, err
		}

		totalDuration += buffer.Delay

		imageBuffers[i] = buffer
	}

	return &Cursor{
		TotalDuration: totalDuration,
		Name:          name,
		Images:        imageBuffers,
	}, nil
}

// Destroy destroys a Cursor
func (c *Cursor) Destroy() (err error) {
	if len(c.Images) > 0 {
		for _, buf := range c.Images {
			err1 := buf.Destroy()
			if err1 != nil && err == nil {
				err = fmt.Errorf("error when destroying cursor: %w", err1)
			}
		}
	}

	return err
}

// nearestImages picks the images whose nominal size is closest to the
// requested one (all animation frames of that size). Upstream only matched
// exact sizes and otherwise fell back to the first image; HiDPI requests
// (e.g. 48 from a theme shipping 24/32/64) want the true nearest.
func nearestImages(size uint32, images []*xcursor.Image) []*xcursor.Image {
	index := 0
	best := int64(1) << 62
	for i, image := range images {
		d := int64(image.Size) - int64(size)
		if d < 0 {
			d = -d
		}
		// ties go to the larger size: downscaling looks better than upscaling
		if d < best || (d == best && image.Size > images[index].Size) {
			best = d
			index = i
		}
	}

	nearestImage := images[index]

	var nImages []*xcursor.Image

	for _, image := range images {
		if image.Width == nearestImage.Width && image.Height == nearestImage.Height {
			nImages = append(nImages, image)
		}
	}

	return nImages
}

// FrameAndDuration carries information about a frame and duration that should be used
type FrameAndDuration struct {
	FrameIndex    int
	FrameDuration uint32
}

// FrameAndDuration informs which frame and duration should be used at a specific time
func (c *Cursor) FrameAndDuration(millis uint32) FrameAndDuration {
	millis %= c.TotalDuration

	res := 0
	for i, img := range c.Images {
		if millis < img.Delay {
			res = i
			break
		}
		millis -= img.Delay
	}

	return FrameAndDuration{
		FrameIndex:    res,
		FrameDuration: millis,
	}
}

// ImageBuffer is a Wayland buffer for cursor
type ImageBuffer struct {
	buffer   *wl.Buffer
	Delay    uint32
	hotspotX uint32
	hotspotY uint32
	width    uint32
	height   uint32
}

// NewImageBuffer creates a new ImageBuffer from Theme and cursor Image.
// The image's raw pixels are already wl_shm ARGB8888 (see xcursor.Image.Pix).
func NewImageBuffer(theme *Theme, image *xcursor.Image) (*ImageBuffer, error) {
	buf := image.Pix
	offset, err := theme.File.Seek(0, 2)
	if err != nil {
		return nil, err
	}

	newSize := offset + int64(len(buf))
	if err2 := theme.grow(int32(newSize)); err2 != nil {
		return nil, err2
	}

	if _, err3 := theme.File.Write(buf); err3 != nil {
		return nil, err3
	}

	buffer, err4 := theme.Pool.CreateBuffer(
		int32(offset),
		int32(image.Width),
		int32(image.Height),
		int32(image.Width)*4,
		wl.ShmFormatArgb8888,
	)
	if err4 != nil {
		return nil, err4
	}

	return &ImageBuffer{
		buffer:   buffer,
		Delay:    image.Delay,
		hotspotX: image.HotspotX,
		hotspotY: image.HotspotY,
		width:    image.Width,
		height:   image.Height,
	}, nil
}

// GetBuffer gets buffer
func (b *ImageBuffer) GetBuffer() *wl.Buffer {
	return b.buffer
}

// ImageCount returns image count
func (c *Cursor) ImageCount() int {
	return len(c.Images)
}

// GetHotspotX gets hotspot x
func (b *ImageBuffer) GetHotspotX() int {
	return int(b.hotspotX)
}

// GetHotspotY gets hotspot Y
func (b *ImageBuffer) GetHotspotY() int {
	return int(b.hotspotY)
}

// GetWidth gets width
func (b *ImageBuffer) GetWidth() int {
	return int(b.width)
}

// GetHeight gets height
func (b *ImageBuffer) GetHeight() int {
	return int(b.height)
}

// Destroy destroys the ImageBuffer
func (b *ImageBuffer) Destroy() error {
	return b.buffer.Destroy()
}

// PointerSetCursor sets Cursor of Pointer
func PointerSetCursor(p *wl.Pointer, serial uint32, pointerSurface *wl.Surface,
	hotspotX int32, hotspotY int32) error {
	return p.SetCursor(serial, pointerSurface, hotspotX, hotspotY)
}
