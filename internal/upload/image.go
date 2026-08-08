package upload

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
	CompanyImageMaxBytes      = int64(5 * 1024 * 1024)
	CompanyImageMaxDimension  = 4096
	CompanyImageMaxPixelCount = int64(16_000_000)
)

type ImageMetadata struct {
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

func ValidateCompanyImage(source ResolvedSource) (ImageMetadata, error) {
	if len(source.Content) == 0 {
		return ImageMetadata{}, errors.New("company image content must not be empty")
	}
	if int64(len(source.Content)) > CompanyImageMaxBytes {
		return ImageMetadata{}, fmt.Errorf("company image exceeds maximum encoded size %d", CompanyImageMaxBytes)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source.Content))
	if err != nil {
		return ImageMetadata{}, errors.New("company image content is not a supported valid raster image")
	}
	policy, ok := imageFormats[format]
	if !ok {
		return ImageMetadata{}, fmt.Errorf("company image format %q is not supported", format)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return ImageMetadata{}, errors.New("company image dimensions must be positive")
	}
	if config.Width > CompanyImageMaxDimension || config.Height > CompanyImageMaxDimension {
		return ImageMetadata{}, fmt.Errorf("company image dimensions must not exceed %dx%d", CompanyImageMaxDimension, CompanyImageMaxDimension)
	}
	if int64(config.Width)*int64(config.Height) > CompanyImageMaxPixelCount {
		return ImageMetadata{}, fmt.Errorf("company image must not exceed %d total pixels", CompanyImageMaxPixelCount)
	}
	if _, decodedFormat, err := image.Decode(bytes.NewReader(source.Content)); err != nil || decodedFormat != format {
		return ImageMetadata{}, errors.New("company image content is not a complete valid raster image")
	}

	extension := strings.ToLower(filepath.Ext(source.Filename))
	if !policy.extensions[extension] {
		return ImageMetadata{}, fmt.Errorf("company image filename extension %q does not match decoded %s content", extension, format)
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(source.ContentType))
	if err != nil || strings.ToLower(mediaType) != policy.contentType {
		return ImageMetadata{}, fmt.Errorf("company image content type must be %s for decoded %s content", policy.contentType, format)
	}
	return ImageMetadata{Format: format, ContentType: policy.contentType, Width: config.Width, Height: config.Height}, nil
}
