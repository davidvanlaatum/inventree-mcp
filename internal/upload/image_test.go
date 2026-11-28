package upload

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedRasterPolicyPreservesCompanyCompatibility(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	var data bytes.Buffer
	r.NoError(png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2, 3))))
	source := ResolvedSource{Filename: "x.png", ContentType: "image/png", Content: data.Bytes()}
	generic, err := ValidateRasterImage(source)
	r.NoError(err)
	company, err := ValidateCompanyImage(source)
	r.NoError(err)
	r.Equal(generic, company)
	r.Equal("png", company.Format)
	source.Content = nil
	_, err = ValidateRasterImage(source)
	r.ErrorContains(err, "image content must not be empty")
	r.NotContains(err.Error(), "company")
	_, err = ValidateCompanyImage(source)
	r.ErrorContains(err, "company image content must not be empty")
}
