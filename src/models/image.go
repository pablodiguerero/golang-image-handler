package models

import (
    "bytes"
    "image"
    "image/color"
    "image/jpeg"
    "io/ioutil"
    "os"
    "path/filepath"

    "github.com/chai2010/webp"
    "github.com/disintegration/imaging"
)

type Image struct {
    Parser ParseResult
}

func (img *Image) LoadOriginal() (*os.File, error) {
    return os.Open(img.Parser.GetFullSizePath())
}

func (img *Image) LoadCached() (*os.File, error) {
    return os.Open(img.Parser.GetCachePath())
}

func (img *Image) MakeCachedImage() (cachedPath string, err error) {
    var handledImage *image.NRGBA

    cachedPath = img.Parser.GetCachePath()
    decodedImage, _ := imaging.Open(img.Parser.GetFullSizePath())

    switch img.Parser.Handle {
    case "fill":
        {
            handledImage = imaging.Fill(decodedImage, img.Parser.Width, img.Parser.Height, imaging.Center, imaging.Box)
            break
        }
    case "fit":
        {
            handledImage = imaging.Fit(decodedImage, img.Parser.Width, img.Parser.Height, imaging.Box)
            break
        }
    case "fitstrict":
        {
            var emptyImage *image.NRGBA = imaging.New(img.Parser.Width, img.Parser.Height, color.White)
            var fittedImage *image.NRGBA = imaging.Fit(decodedImage, img.Parser.Width, img.Parser.Height, imaging.Box)

            handledImage = imaging.PasteCenter(emptyImage, fittedImage)
        }
    }

    buf := new(bytes.Buffer)

    if img.Parser.AllowedWebp {
        if err = webp.Encode(buf, handledImage, &webp.Options{Quality: float32(img.Parser.Quality)}); err != nil {
            return "", err
        }
    } else {
        if err = jpeg.Encode(buf, handledImage, &jpeg.Options{Quality: img.Parser.Quality}); err != nil {
            return "", err
        }
    }

    if err = os.MkdirAll(filepath.Dir(cachedPath), os.ModePerm); err != nil {
        return "", err
    }

    if err = ioutil.WriteFile(cachedPath, buf.Bytes(), os.ModePerm); err != nil {
        return "", err
    }

    return
}

func LoadImage(path string, allowedWebp ...bool) (img Image) {
    var err error
    var parseResult ParseResult

    allowedWebpLocal := false

    if len(allowedWebp) > 0 {
        allowedWebpLocal = allowedWebp[0]
    }

    if parseResult, err = ParsePath(path, allowedWebpLocal); err != nil {
        panic(err)
    }

    img = Image{Parser: parseResult}

    if _, err = img.LoadOriginal(); err != nil {
        panic(err)
    }

    return img
}
