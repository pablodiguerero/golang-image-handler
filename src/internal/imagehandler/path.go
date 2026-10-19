package imagehandler

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Both separators are accepted. Early deployments documented the dot form
// (photo.fill-200x200.jpg), while deployed versions also use a hyphen.
// A basename may contain Unicode: media libraries commonly retain the
// uploader's filename (for example, a Cyrillic product name).
var transformedFilePattern = regexp.MustCompile(`(?i)^(.+)[.-](fill|fit|fitstrict|shrink)-([0-9]+)x([0-9]+)(?:-q([0-9]+))?\.(jpg|jpeg|png|webp|avif|gif|tif|tiff)$`)
var originalFilePattern = regexp.MustCompile(`(?i)^(.+)\.original\.(jpg|jpeg|png|webp|avif|gif|tif|tiff)$`)

var errInvalidPath = errors.New("invalid image path")

// errNotAllowed marks a well-formed path whose variant is not listed in
// APP_ALLOWED_SIZES. The HTTP handler answers it with 404, not 400.
var errNotAllowed = errors.New("image variant is not allowed")

type requestSpec struct {
	sourceRelative string
	operation      string
	width          int
	height         int
	quality        int
	// qualitySet is true when the URL carried an explicit -q value.
	qualitySet bool
	// zeroWidth and zeroHeight mark an unbounded side of a shrink request. The
	// width and height fields then hold the substituted maximum dimension.
	zeroWidth, zeroHeight bool
}

func parsePath(urlPath string, maxDimension, maxPixels int) (requestSpec, error) {
	// net/http and a reverse proxy may each decode a request path once. Existing
	// templates also contain paths escaped twice (for example %25D0%25A2...).
	// Decode at most two remaining layers before validation and file resolution.
	// Validation below still rejects traversal introduced by either decoding.
	for range 2 {
		decodedPath, err := url.PathUnescape(urlPath)
		if err != nil {
			break // A literal percent sign in a legacy filename is valid.
		}
		if decodedPath == urlPath {
			break
		}
		urlPath = decodedPath
	}
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
	if err != nil {
		return requestSpec{}, errInvalidPath
	}
	operation := strings.ToLower(match[2])
	var zeroWidth, zeroHeight bool
	if operation == "shrink" {
		// Only shrink accepts 0 for one side: that side is unbounded. Both
		// sides at 0 would not describe any box.
		if width == 0 && height == 0 {
			return requestSpec{}, errInvalidPath
		}
		if width == 0 {
			zeroWidth, width = true, maxDimension
		}
		if height == 0 {
			zeroHeight, height = true, maxDimension
		}
	}
	if width < 1 || height < 1 || width > maxDimension || height > maxDimension || width > maxPixels/height {
		return requestSpec{}, errInvalidPath
	}
	quality := 100 // Existing URLs without -q use JPEG/WebP quality 100.
	qualitySet := match[5] != ""
	if qualitySet {
		quality, err = strconv.Atoi(match[5])
		if err != nil || quality < 1 || quality > 100 {
			return requestSpec{}, errInvalidPath
		}
	}
	parts[len(parts)-1] = match[1] + "." + match[6]
	return requestSpec{
		sourceRelative: path.Join(parts...),
		operation:      operation,
		width:          width,
		height:         height,
		quality:        quality,
		qualitySet:     qualitySet,
		zeroWidth:      zeroWidth,
		zeroHeight:     zeroHeight,
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

// token is the APP_ALLOWED_SIZES form of the variant: quality is not part of it
// and the separator is always the hyphen, whatever the URL used.
func (s requestSpec) token() string {
	if s.operation == "original" {
		return "original"
	}
	width, height := s.width, s.height
	if s.zeroWidth {
		width = 0
	}
	if s.zeroHeight {
		height = 0
	}
	return fmt.Sprintf("%s-%dx%d", s.operation, width, height)
}

var allowedTokenPattern = regexp.MustCompile(`^(fill|fit|fitstrict|shrink)-([0-9]+)x([0-9]+)$`)

// parseAllowedSizes reads APP_ALLOWED_SIZES. An empty value allows everything
// and yields a nil set.
func parseAllowedSizes(value string) (map[string]bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	allowed := make(map[string]bool)
	for _, raw := range strings.Split(value, ",") {
		token := strings.ToLower(strings.TrimSpace(raw))
		if token == "original" {
			allowed[token] = true
			continue
		}
		match := allowedTokenPattern.FindStringSubmatch(token)
		if match == nil {
			return nil, fmt.Errorf("APP_ALLOWED_SIZES: invalid token %q (want <fill|fit|fitstrict|shrink>-<W>x<H> or original)", raw)
		}
		width, errW := strconv.Atoi(match[2])
		height, errH := strconv.Atoi(match[3])
		if errW != nil || errH != nil || width < 0 || height < 0 {
			return nil, fmt.Errorf("APP_ALLOWED_SIZES: invalid size in token %q", raw)
		}
		if match[1] == "shrink" {
			if width == 0 && height == 0 {
				return nil, fmt.Errorf("APP_ALLOWED_SIZES: token %q has both sides 0", raw)
			}
		} else if width < 1 || height < 1 {
			return nil, fmt.Errorf("APP_ALLOWED_SIZES: token %q must have both sides >= 1", raw)
		}
		allowed[fmt.Sprintf("%s-%dx%d", match[1], width, height)] = true
	}
	return allowed, nil
}

// parseDefaultQuality reads APP_DEFAULT_QUALITY; unset means 100.
func parseDefaultQuality(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 100, nil
	}
	quality, err := strconv.Atoi(value)
	if err != nil || quality < 1 || quality > 100 {
		return 0, fmt.Errorf("APP_DEFAULT_QUALITY must be an integer from 1 to 100, got %q", value)
	}
	return quality, nil
}

// parseRequest parses the path and applies the per-deployment settings: the
// variant allowlist and the default quality for URLs without -q.
func (h *Handler) parseRequest(urlPath string) (requestSpec, error) {
	spec, err := parsePath(urlPath, h.maxDimension, h.maxPixels)
	if err != nil {
		return requestSpec{}, err
	}
	if h.allowedSizes != nil && !h.allowedSizes[spec.token()] {
		return requestSpec{}, errNotAllowed
	}
	if !spec.qualitySet && h.defaultQuality > 0 {
		spec.quality = h.defaultQuality
	}
	return spec, nil
}
