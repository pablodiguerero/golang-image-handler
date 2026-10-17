//go:build !linux

package imagehandler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Non-Linux development platforms do not provide openat2. Resolve the path
// before opening it; production images use secure_open_linux.go.
func openSourceWithinRoot(root, source string) (*os.File, error) {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("source image escapes image root")
	}
	return os.Open(resolved)
}
