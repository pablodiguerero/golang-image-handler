package imagehandler

import (
	"errors"
	"fmt"
	"mime"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Both separators are accepted. Early deployments documented the dot form
// (photo.fill-200x200.jpg), while deployed versions also use a hyphen.
// A basename may contain Unicode: media libraries commonly retain the
// uploader's filename (for example, a Cyrillic product name).
var transformedFilePattern = regexp.MustCompile(`(?i)^(.+)[.-](fill|fit|fitstrict)-([0-9]+)x([0-9]+)(?:-q([0-9]+))?\.(jpg|jpeg|png|webp|avif)$`)
var originalFilePattern = regexp.MustCompile(`(?i)^(.+)\.original\.(jpg|jpeg|png|webp|avif)$`)

var errInvalidPath = errors.New("invalid image path")

type requestSpec struct {
	sourceRelative string
	operation      string
	width          int
	height         int
	quality        int
}

func parsePath(urlPath string, maxDimension, maxPixels int) (requestSpec, error) {
	if !strings.HasPrefix(urlPath, "/images/") {
		return requestSpec{}, errInvalidPath
	}
	relative := strings.TrimPrefix(urlPath, "/images/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 {
		return requestSpec{}, errInvalidPath
	}
	for _, segment := range parts[:len(parts)-1] {
		if !safeSegment(segment) {
			return requestSpec{}, errInvalidPath
		}
	}
	filename := parts[len(parts)-1]
	if match := originalFilePattern.FindStringSubmatch(filename); match != nil {
		if !safeBasename(match[1]) {
			return requestSpec{}, errInvalidPath
		}
		parts[len(parts)-1] = match[1] + "." + match[2]
		return requestSpec{
			sourceRelative: path.Join(parts...),
			operation:      "original",
			quality:        100,
		}, nil
	}
	match := transformedFilePattern.FindStringSubmatch(filename)
	if match == nil {
		return requestSpec{}, errInvalidPath
	}
	if !safeBasename(match[1]) {
		return requestSpec{}, errInvalidPath
	}
	width, err := strconv.Atoi(match[3])
	if err != nil {
		return requestSpec{}, errInvalidPath
	}
	height, err := strconv.Atoi(match[4])
	if err != nil || width < 1 || height < 1 || width > maxDimension || height > maxDimension || width > maxPixels/height {
		return requestSpec{}, errInvalidPath
	}
	quality := 100 // Existing URLs without -q use JPEG/WebP quality 100.
	if match[5] != "" {
		quality, err = strconv.Atoi(match[5])
		if err != nil || quality < 1 || quality > 100 {
			return requestSpec{}, errInvalidPath
		}
	}
	parts[len(parts)-1] = match[1] + "." + match[6]
	return requestSpec{
		sourceRelative: path.Join(parts...),
		operation:      strings.ToLower(match[2]),
		width:          width,
		height:         height,
		quality:        quality,
	}, nil
}

func safeBasename(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, 0)
}

func safeSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	// Directory names used by existing sites can contain any URL-safe character.
	// Splitting the path and rejecting traversal components is sufficient because
	// source() verifies the resolved path remains beneath APP_IMAGE_ROOT.
	return !strings.ContainsRune(segment, 0)
}

type outputFormat string

const (
	formatJPEG outputFormat = "jpg"
	formatWebP outputFormat = "webp"
	formatAVIF outputFormat = "avif"
)

func (f outputFormat) contentType() string {
	switch f {
	case formatWebP:
		return "image/webp"
	case formatAVIF:
		return "image/avif"
	default:
		return "image/jpeg"
	}
}

// A format must be named explicitly in Accept. Wildcards are not evidence that
// the client can decode AVIF or WebP.
func negotiateFormat(accept string, allowAVIF, allowWebP bool) outputFormat {
	accepted := make(map[string]float64)
	for _, item := range strings.Split(accept, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil {
			continue
		}
		q := 1.0
		if raw, ok := params["q"]; ok {
			q, err = strconv.ParseFloat(raw, 64)
			if err != nil || q < 0 || q > 1 {
				continue
			}
		}
		if q > accepted[mediaType] {
			accepted[mediaType] = q
		}
	}
	if allowAVIF && accepted["image/avif"] > 0 && accepted["image/avif"] >= accepted["image/webp"] {
		return formatAVIF
	}
	if allowWebP && accepted["image/webp"] > 0 {
		return formatWebP
	}
	return formatJPEG
}

func (s requestSpec) String() string {
	return fmt.Sprintf("%s:%s:%dx%d:q%d", s.sourceRelative, s.operation, s.width, s.height, s.quality)
}
