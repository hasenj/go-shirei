package slay

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func decodeImage(content []byte) *image.RGBA {
	img, _, _ := image.Decode(bytes.NewReader(content))
	return imageToRGBA(img)
}

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

var imageIds = make([]*image.RGBA, 1, 1024) // first image is the zero image!
var imageIdByPath = make(map[string]ImageId)

func LoadImage(fpath string) *image.RGBA {
	const key = "image"
	img, found := _getFileCacheContent[*image.RGBA](fpath, key)
	if found {
		return img
	}

	content := ReadFileContent(fpath)
	img = decodeImage(content)
	_setFileCacheContent(fpath, key, img)

	// write the image to the image ids list
	// note: the way this is currently setup, when the image on disk changes,
	// its imageid will point to the new version!
	// this means it's not straight forward to load an image and just keep it there!
	// (unless you do it via some mechanis, other than this LoadImage function)
	imageId := imageIdByPath[fpath]
	if imageId == 0 {
		imageIdByPath[fpath] = ImageId(len(imageIds))
		imageIds = append(imageIds, img)
	} else {
		imageIds[imageId] = img
	}

	return img
}

// this function is mostly for the backend
func LookupImage(id ImageId) *image.RGBA {
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
	bounds := img.Bounds()
	size := Vec2{float32(bounds.Dx()), float32(bounds.Dy())}
	var scaleX, scaleY float32 = 1, 1

	if maxSize[0] > 0 && maxSize[0] < size[0] {
		scaleX = maxSize[0] / size[0]
	}
	if maxSize[1] > 0 && maxSize[1] < size[1] {
		scaleY = maxSize[1] / size[1]
	}
	scale := min(scaleX, scaleY)
	size = Vec2Mul(size, scale)

	Layout(Attrs{MaxSize: size, MinSize: size, Clip: true}, func() {
		current.imageId = GetImageId(fpath)
	})
}
