package shirei

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	_ "golang.org/x/image/webp"
)

// from: https://stackoverflow.com/a/61721655/35364
func imageToRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}

	// No conversion needed if image is an *image.RGBA.
	if dst, ok := src.(*image.RGBA); ok {
		return dst
	}

	// Use the image/draw package to convert to *image.RGBA.
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// FIXME we need to manage images in a way that allows them to be added and removed dynamically without interfering with caching!
// in other words, we need to use a handles system!
type ImageId uint32

var imageIds = make([]*ImageData, 1, 1024) // first image is the zero image!
var imageIdByPath = make(map[string]ImageId)

type ImageData struct {
	image.Config
	image.RGBA
}

func LoadImageConfig(fpath string) image.Config {
	const key = "image-config"
	cfg, found := _getFileCacheContent[image.Config](fpath, key)
	if found {
		return cfg
	}
	f, _ := os.Open(fpath)
	defer f.Close()
	cfg, _, _ = image.DecodeConfig(f)
	_setFileCacheContent(fpath, key, cfg)
	return cfg
}

func LoadImage(fpath string) *ImageData {
	const key = "image"
	img, found := _getFileCacheContent[*ImageData](fpath, key)
	if found {
		return img
	}

	img = new(ImageData)
	content := ReadFileContent(fpath)

	// read just the header
	img.Config, _, _ = image.DecodeConfig(bytes.NewReader(content))

	const threshold = 500 * 1024
	if len(content) < threshold {
		// small enough size; load immediately
		decoded, _, _ := image.Decode(bytes.NewReader(content))
		rgba := imageToRGBA(decoded)
		if rgba != nil {
			img.RGBA = *rgba
		}
	} else {
		// defer loading to background
		go func() {
			decoded, _, _ := image.Decode(bytes.NewReader(content))
			rgba := imageToRGBA(decoded)
			if rgba != nil {
				// log.Println("Loaded", fpath)
				// log.Println("Image config size:", img.Config.Width, img.Config.Height)
				// log.Println("Image actual size:", rgba.Bounds().Dx(), rgba.Bounds().Dy())
				WithFrameLock(func() {
					img.RGBA = *rgba
					RequestNextFrame()
				})
			}
		}()
	}

	_setFileCacheContent(fpath, key, img)

	// write the image to the image ids list
	// note: the way this is currently setup, when the image on disk changes,
	// its imageid will point to the new version!
	// this means it's not straight forward to load an image and just keep it there!
	// (unless you do it via some mechanis, other than this LoadImage function)
	imageId := imageIdByPath[fpath]
	if imageId == 0 {
		// FIXME: we should only set this after the image is actually loaded!
		// the way we currently do it violate the unwritten assumption that an
		// unchanged surface data indicates unchanged pixel output!
		imageIdByPath[fpath] = ImageId(len(imageIds))
		imageIds = append(imageIds, img)
	} else {
		imageIds[imageId] = img
	}

	return img
}

// this function is mostly for the backend
func LookupImage(id ImageId) *ImageData {
	return imageIds[int(id)]
}

func GetImageId(fpath string) ImageId {
	return imageIdByPath[fpath]
}

// FIXME: we shuold be able to also specify minSize and border radius, perhaps border color too!
func Image(fpath string, maxSize Vec2) {
	img := LoadImage(fpath)
	if img == nil {
		// FIXME: use a default non-sensical white image or something
		return
	}
	size := Vec2{f32(img.Config.Width), f32(img.Config.Height)}
	size = RestrictedSize(size, maxSize)
	Layout(Attrs{MaxSize: size, MinSize: size, Clip: true}, func() {
		current.imageId = GetImageId(fpath)
	})
}

func RestrictedSize(size Vec2, maxSize Vec2) Vec2 {
	var scaleX, scaleY float32 = 1, 1

	if maxSize[0] > 0 && maxSize[0] < size[0] {
		scaleX = maxSize[0] / size[0]
	}
	if maxSize[1] > 0 && maxSize[1] < size[1] {
		scaleY = maxSize[1] / size[1]
	}
	scale := min(scaleX, scaleY)
	return Vec2Mul(size, scale)
}
