// Package raster validates bounded image bytes without transport, telemetry or filesystem dependencies.
package raster

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxBytes      = int64(5 * 1024 * 1024)
	MaxDimension  = 4096
	MaxPixelCount = int64(16_000_000)
)

type Source struct {
	Filename    string
	ContentType string
	Content     []byte
}

type Metadata struct {
	Format      string
	ContentType string
	Width       int
	Height      int
}

var imageFormats = map[string]struct {
	contentType string
	extensions  map[string]bool
}{
	"png":  {contentType: "image/png", extensions: map[string]bool{".png": true}},
	"jpeg": {contentType: "image/jpeg", extensions: map[string]bool{".jpg": true, ".jpeg": true}},
	"webp": {contentType: "image/webp", extensions: map[string]bool{".webp": true}},
}

// ValidateRasterImage is the shared bounded raster policy for company and note images.
func Validate(source Source) (Metadata, error) {
	if len(source.Content) == 0 {
		return Metadata{}, errors.New("image content must not be empty")
	}
	if int64(len(source.Content)) > MaxBytes {
		return Metadata{}, fmt.Errorf("image exceeds maximum encoded size %d", MaxBytes)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source.Content))
	if err != nil {
		return Metadata{}, errors.New("image content is not a supported valid raster image")
	}
	policy, ok := imageFormats[format]
	if !ok {
		return Metadata{}, fmt.Errorf("image format %q is not supported", format)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return Metadata{}, errors.New("image dimensions must be positive")
	}
	if config.Width > MaxDimension || config.Height > MaxDimension {
		return Metadata{}, fmt.Errorf("image dimensions must not exceed %dx%d", MaxDimension, MaxDimension)
	}
	if int64(config.Width)*int64(config.Height) > MaxPixelCount {
		return Metadata{}, fmt.Errorf("image must not exceed %d total pixels", MaxPixelCount)
	}
	if _, decodedFormat, err := image.Decode(bytes.NewReader(source.Content)); err != nil || decodedFormat != format {
		return Metadata{}, errors.New("image content is not a complete valid raster image")
	}

	extension := strings.ToLower(filepath.Ext(source.Filename))
	if !policy.extensions[extension] {
		return Metadata{}, fmt.Errorf("image filename extension %q does not match decoded %s content", extension, format)
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(source.ContentType))
	if err != nil || strings.ToLower(mediaType) != policy.contentType {
		return Metadata{}, fmt.Errorf("image content type must be %s for decoded %s content", policy.contentType, format)
	}
	return Metadata{Format: format, ContentType: policy.contentType, Width: config.Width, Height: config.Height}, nil
}
