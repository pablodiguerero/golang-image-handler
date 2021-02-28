package common

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	"image-handler.it-lab.su/utils"
)

// ImagePath - image path structure
type ImagePath struct {
	folder string
	name   string
	exten  string
}

// ImageSize - common image structure
type ImageSize struct {
	width  int
	height int
}

// HandleMethod - handle image structure
// Valid options are:
//  * original - return original image
//  * fill-100x100 - return image thumbnail 100x100 px
//  * fit-100x100 - return croped image 100x100 px
type HandleMethod struct {
	method string
	size   ImageSize
}

// Image - common image structure
type Image struct {
	relativePath string
	handleMethod HandleMethod
	path         ImagePath
	body         []byte
	rendition    map[string]string
    allowedWebp  bool
}

// ToString - convert handle struct to string
func (handle *HandleMethod) ToString() string {
	if 0 == handle.size.width || 0 == handle.size.height {
		return handle.method
	}

	return fmt.Sprintf("%s-%dx%d", handle.method, handle.size.width, handle.size.height)
}

// GetRelativeNaturalPath - return image path
func (img *Image) GetRelativeNaturalPath() string {
	if len(img.relativePath) == 0 {
		img.relativePath = fmt.Sprintf("%s/%s.%s", img.path.folder, img.path.name, img.path.exten)
	}

	return img.relativePath
}

// GetRelativePath - return image path
func (img *Image) GetRelativePath() string {
    if (img.allowedWebp) {
        return fmt.Sprintf("%s/%s.%s.%s", img.path.folder, img.path.name, img.handleMethod.ToString(), "webp")
    } else {
        return fmt.Sprintf("%s/%s.%s.%s", img.path.folder, img.path.name, img.handleMethod.ToString(), img.path.exten)
    }
}

// GetAbsolutePath - return image absolute path
func (img *Image) GetAbsolutePath() string {
	return fmt.Sprintf("%s/%s", utils.GetImageRoot(), img.GetRelativeNaturalPath())
}

// GetCachePath - return absolute image cache path
func (img *Image) GetCachePath() string {
	if "original" == img.handleMethod.ToString() {
		return ""
	}

	return fmt.Sprintf("%s/%s", utils.GetImageCache(), img.GetRelativePath())
}

// GetBody - return image body bytes
func (img *Image) GetBody() []byte {
	return img.body
}

// Cache image
func (img *Image) Cache() (err error) {
	var cachePathRelative string = img.GetCachePath()

	if len(utils.GetImageCache()) == 0 || len(img.body) == 0 || len(cachePathRelative) == 0 {
		return
	}

	if err = os.MkdirAll(filepath.Dir(img.GetCachePath()), os.ModePerm); err != nil {
		return err
	}

	if err = ioutil.WriteFile(img.GetCachePath(), img.GetBody(), os.ModePerm); err != nil {
		return err
	}

	return
}

// LoadCache - load image from cache
func (img *Image) LoadCache() bool {
	if _, err := os.Stat(img.GetCachePath()); os.IsNotExist(err) {
		return false
	}

	img.body, _ = ioutil.ReadFile(img.GetCachePath())

	return true
}

// PrepareHandling - prepare image handling values
func (img *Image) PrepareHandling(handleValue string) (err error) {
	if "original" == handleValue {
		img.handleMethod = HandleMethod{method: "original"}
		return
	}

	re := regexp.MustCompile(`^(fill|fit|fitstrict)\-([0-9]+)x([0-9]+)$`)
	result := re.FindStringSubmatch(handleValue)

	if len(result) > 0 {
		width, errWidth := strconv.Atoi(result[2])
		height, errHeight := strconv.Atoi(result[3])

		if errWidth == nil && errHeight == nil {
			img.handleMethod = HandleMethod{
				method: result[1],
				size: ImageSize{
					width:  width,
					height: height,
				},
			}
		}
	}

	return
}

// IsAllowedWebp - Is webp supported?
func (img *Image) IsAllowedWebp() (response bool) {
    response = img.allowedWebp;
    return
}

// Handle - handeled current image
func (img *Image) Handle() (err error) {
	var handledImage *image.NRGBA
	decodedImage, _ := imaging.Open(img.GetAbsolutePath())

	defer func() {
		if e := recover(); e != nil {
			err = errors.New("Error handling image")
			log.Println(e)
		}
	}()

	switch img.handleMethod.method {
	case "fill":
		{
			handledImage = imaging.Fill(decodedImage, img.handleMethod.size.width, img.handleMethod.size.height, imaging.Center, imaging.Box)
			break
		}
	case "fit":
		{
			handledImage = imaging.Fit(decodedImage, img.handleMethod.size.width, img.handleMethod.size.height, imaging.Box)
			break
		}
	case "original":
		{
			handledImage = imaging.Clone(decodedImage)
			break
		}
	case "fitstrict":
		{
			var emptyImage *image.NRGBA = imaging.New(img.handleMethod.size.width, img.handleMethod.size.height, color.White)
			var fittedImage *image.NRGBA = imaging.Fit(decodedImage, img.handleMethod.size.width, img.handleMethod.size.height, imaging.Box)

			handledImage = imaging.PasteCenter(emptyImage, fittedImage)
		}
	}

	buf := new(bytes.Buffer)

    if (img.allowedWebp) {
        err = webp.Encode(buf, handledImage, &webp.Options{Lossless: true})
    } else {
	    err = jpeg.Encode(buf, handledImage, nil)
    }

	img.body = buf.Bytes()

	return
}

// GetImage - return common image data like bucket, filename, relative path, etc
func GetImage(path string, name string, handleMethod string, exten string, allowedWebp bool) (elements Image, err error) {
	var localImg Image = Image{
		path: ImagePath{folder: path, name: name, exten: exten},
        allowedWebp: allowedWebp,
	}

	if err = localImg.PrepareHandling(handleMethod); err != nil {
		return Image{}, err
	}

	if _, err := os.Stat(localImg.GetAbsolutePath()); os.IsNotExist(err) {
		log.Printf("Not loaded %q", localImg.GetRelativePath())
		return Image{}, err
	}

	return localImg, nil
}
