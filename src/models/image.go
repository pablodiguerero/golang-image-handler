package models

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
)

type ParseResult struct {
	path     string
	baseName string
	exten    string
	width    int
	height   int
	handle   string
}

func (result ParseResult) getFullSizePath(exten string) string {
	path := fmt.Sprintf("%s/%s.%s", result.path, result.baseName, exten)

	if os.Getenv("APP_IMAGE_ROOT") != "" {
		return fmt.Sprintf("%s/%s", os.Getenv("APP_IMAGE_ROOT"), path)
	}

	return path
}

func (result ParseResult) getCacheResult(exten string) string {
	path := fmt.Sprintf("%s/%s-%s-%dx%d.%s", result.path, result.baseName, result.handle, result.width, result.height, exten)

	if os.Getenv("APP_IMAGE_CACHE") != "" {
		return fmt.Sprintf("%s/%s", os.Getenv("APP_IMAGE_CACHE"), path)
	}

	return path
}

func (result ParseResult) GetOriginalPath() string {
	return result.getFullSizePath(result.exten)
}

func (result ParseResult) GetFullSizeJpgPath() string {
	return result.getFullSizePath("jpg")
}

func (result ParseResult) GetFullSizeWebpPath() string {
	return result.getFullSizePath("webp")
}

func (result ParseResult) GetCachedJpgPath() string {
	return result.getCacheResult("jpg")
}

func (result ParseResult) GetCachedWebpPath() string {
	return result.getCacheResult("webp")
}

func parsePath(path string) (ParseResult, error) {
	var width int
	var height int

	re := regexp.MustCompile(`(?i)(.+)\/([a-z0-9_\-]+)\-(fill|fit|fitstrict)\-([0-9]+)x([0-9]+)\.(jpg|png|jpeg|webp)$`)
	regexpResult := re.FindStringSubmatch(path)

	if len(regexpResult) != 7 {
		return ParseResult{}, errors.New("ErrorWhilePathParse")
	}

	width, _ = strconv.Atoi(regexpResult[4])
	height, _ = strconv.Atoi(regexpResult[5])

	return ParseResult{
		path:     regexpResult[1],
		baseName: regexpResult[2],
		exten:    regexpResult[6],
		width:    width,
		height:   height,
		handle:   regexpResult[3],
	}, nil
}

// Image - common Image structure
type Image struct {
	Parser ParseResult
}

func (img *Image) LoadOriginal() (*os.File, error) {
	return os.Open(img.Parser.GetOriginalPath())
}

func (img *Image) LoadCachedJpg() (*os.File, error) {
	return os.Open(img.Parser.GetCachedJpgPath())
}

func (img *Image) LoadCachedWebp() (*os.File, error) {
	return os.Open(img.Parser.GetCachedWebpPath())
}

func (img *Image) ConvertOriginalToJpeg() (err error) {
    jpegPath := img.Parser.GetFullSizeJpgPath()
    file, _ := imaging.Open(img.Parser.GetFullSizeWebpPath())

	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, file, &jpeg.Options{Quality: 100})

    if err != nil {
        return err
    }

    if err := os.MkdirAll(filepath.Dir(jpegPath), os.ModePerm); err != nil {
		return err
	}

    if err = ioutil.WriteFile(jpegPath, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}

    return nil;
}

func (img *Image) ConvertOriginalToWebp() (err error) {
    webpPath := img.Parser.GetFullSizeWebpPath()
    file, _ := imaging.Open(img.Parser.GetFullSizeJpgPath())

	buf := new(bytes.Buffer)
	err = webp.Encode(buf, file, &webp.Options{Quality: 100})

    log.Print(webpPath)

    if err != nil {
        return err
    }

    if err := os.MkdirAll(filepath.Dir(webpPath), os.ModePerm); err != nil {
		return err
	}

    if err = ioutil.WriteFile(webpPath, buf.Bytes(), os.ModePerm); err != nil {
		return err
	}

    return nil;
}

func (img *Image) prepareOriginals() {
    _, webpStatErr := os.Stat(img.Parser.GetFullSizeWebpPath())
    _, jpegStatErr := os.Stat(img.Parser.GetFullSizeJpgPath())

    if img.Parser.exten == "webp" && os.IsNotExist(jpegStatErr) {
        if err := img.ConvertOriginalToJpeg(); err != nil {
            panic(err)
        }
    }

    if img.Parser.exten != "webp" && os.IsNotExist(webpStatErr) {
        if err := img.ConvertOriginalToWebp(); err != nil {
            panic(err)
        }
    }
}

func (img *Image) MakeCachedImage(format string) (path string) {
    var err error
	var handledImage *image.NRGBA
    var cachedPath string = img.Parser.GetCachedJpgPath()

    if format == "webp" {
        cachedPath = img.Parser.GetCachedWebpPath()
    }

    img.prepareOriginals()

	decodedImage, _ := imaging.Open(img.Parser.GetFullSizeJpgPath())

	switch img.Parser.handle {
	case "fill":
		{
			handledImage = imaging.Fill(decodedImage, img.Parser.width, img.Parser.height, imaging.Center, imaging.Box)
			break
		}
	case "fit":
		{
			handledImage = imaging.Fit(decodedImage, img.Parser.width, img.Parser.height, imaging.Box)
			break
		}
	case "fitstrict":
		{
			var emptyImage *image.NRGBA = imaging.New(img.Parser.width, img.Parser.height, color.White)
			var fittedImage *image.NRGBA = imaging.Fit(decodedImage, img.Parser.width, img.Parser.width, imaging.Box)

			handledImage = imaging.PasteCenter(emptyImage, fittedImage)
		}
	}

	buf := new(bytes.Buffer)

    if format == "webp" {
        if err = webp.Encode(buf, handledImage, &webp.Options{Quality: 100}); err != nil {
            panic(err)
        }
    } else {
        if err = jpeg.Encode(buf, handledImage, &jpeg.Options{Quality: 100}); err != nil {
            panic(err)
        }
    }

	if err = os.MkdirAll(filepath.Dir(cachedPath), os.ModePerm); err != nil {
		panic(err)
	}

	if err = ioutil.WriteFile(cachedPath, buf.Bytes(), os.ModePerm); err != nil {
		panic(err)
	}

	return cachedPath
}

// Load - loading image and get Reader ref
func LoadImage(path string) (img Image) {
    var err error
	var parseResult ParseResult

	if parseResult, err = parsePath(path); err != nil {
		panic(err)
	}

	img = Image{Parser: parseResult}

	if _, err = img.LoadOriginal(); err != nil {
		panic(err)
	}

	return img
}
