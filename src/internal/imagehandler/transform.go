package imagehandler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// libvips can shrink JPEG/WebP on load, so the Go process never holds a decoded
// full-size image. The fitstrict path needs a second pass to add white padding.
func transformImage(ctx context.Context, source, output string, spec requestSpec, format outputFormat) error {
	if spec.operation == "original" {
		return runVips(ctx, "vips", "copy", source, output+saverOptions(spec.quality, format))
	}
	size := fmt.Sprintf("%dx%d", spec.width, spec.height)
	if spec.operation == "fitstrict" {
		intermediate := filepath.Join(filepath.Dir(output), "fitted.v")
		if err := runVips(ctx, "vipsthumbnail", source, "--size", size, "--output", intermediate); err != nil {
			return err
		}
		return runVips(ctx, "vips", "gravity", intermediate, output+saverOptions(spec.quality, format), "centre",
			fmt.Sprint(spec.width), fmt.Sprint(spec.height), "--extend", "white")
	}
	args := []string{source, "--size", size, "--output", output + saverOptions(spec.quality, format)}
	if spec.operation == "fill" {
		args = append(args, "--smartcrop", "centre")
	}
	return runVips(ctx, "vipsthumbnail", args...)
}

func saverOptions(quality int, format outputFormat) string {
	if format == formatAVIF {
		return fmt.Sprintf("[Q=%d,effort=2]", quality)
	}
	return fmt.Sprintf("[Q=%d]", quality)
}

func runVips(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "VIPS_CONCURRENCY=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
