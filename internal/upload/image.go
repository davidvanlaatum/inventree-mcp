package upload

import (
	"fmt"

	"github.com/davidvanlaatum/inventree-mcp/internal/raster"
)

const (
	CompanyImageMaxBytes      = raster.MaxBytes
	CompanyImageMaxDimension  = raster.MaxDimension
	CompanyImageMaxPixelCount = raster.MaxPixelCount
)

type ImageMetadata = raster.Metadata

// ValidateRasterImage applies the shared transport-independent raster policy.
func ValidateRasterImage(source ResolvedSource) (ImageMetadata, error) {
	return raster.Validate(raster.Source{Filename: source.Filename, ContentType: source.ContentType, Content: source.Content})
}

// ValidateCompanyImage retains the established company-image error vocabulary.
func ValidateCompanyImage(source ResolvedSource) (ImageMetadata, error) {
	metadata, err := ValidateRasterImage(source)
	if err != nil {
		return ImageMetadata{}, fmt.Errorf("company %w", err)
	}
	return metadata, nil
}
