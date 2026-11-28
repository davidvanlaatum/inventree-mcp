package raster

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAcceptsApprovedMatchingRasterFormats(t *testing.T) {
	t.Parallel()

	pngBytes := encodeTestImage(t, "png")
	jpegBytes := encodeTestImage(t, "jpeg")
	webpBytes := testWebPBytes(t)

	tests := []struct {
		name, filename, contentType, format string
		content                             []byte
	}{
		{name: "png", filename: "logo.PNG", contentType: "image/png; charset=binary", format: "png", content: pngBytes},
		{name: "jpeg", filename: "logo.jpeg", contentType: "image/jpeg", format: "jpeg", content: jpegBytes},
		{name: "webp", filename: "logo.webp", contentType: "image/webp", format: "webp", content: webpBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			metadata, err := Validate(Source{Filename: tt.filename, ContentType: tt.contentType, Content: tt.content})
			r.NoError(err)
			a.Equal(tt.format, metadata.Format)
			a.Positive(metadata.Width)
			a.Positive(metadata.Height)
		})
	}
}

func TestValidateRejectsUnsafeOrMismatchedContent(t *testing.T) {
	t.Parallel()
	pngBytes := encodeTestImage(t, "png")
	jpegBytes := encodeTestImage(t, "jpeg")
	webpBytes := testWebPBytes(t)
	gifBytes := encodeTestImage(t, "gif")
	tests := []struct {
		name string
		src  Source
		want string
	}{
		{name: "unsupported gif", src: Source{Filename: "logo.gif", ContentType: "image/gif", Content: gifBytes}, want: "not supported"},
		{name: "empty", src: Source{Filename: "logo.png", ContentType: "image/png"}, want: "must not be empty"},
		{name: "oversized bytes", src: Source{Filename: "logo.png", ContentType: "image/png", Content: make([]byte, MaxBytes+1)}, want: "maximum encoded size"},
		{name: "invalid raster", src: Source{Filename: "logo.png", ContentType: "image/png", Content: []byte("not an image")}, want: "not a supported valid raster image"},
		{name: "truncated raster", src: Source{Filename: "logo.png", ContentType: "image/png", Content: pngHeader(2, 3)}, want: "not a complete valid raster image"},
		{name: "truncated jpeg", src: Source{Filename: "logo.jpg", ContentType: "image/jpeg", Content: truncatedRasterAfterConfig(t, jpegBytes)}, want: "not a complete valid raster image"},
		{name: "truncated webp", src: Source{Filename: "logo.webp", ContentType: "image/webp", Content: truncatedRasterAfterConfig(t, webpBytes)}, want: "not a complete valid raster image"},
		{name: "wrong extension", src: Source{Filename: "logo.jpg", ContentType: "image/png", Content: pngBytes}, want: "does not match decoded png"},
		{name: "missing extension", src: Source{Filename: "logo", ContentType: "image/png", Content: pngBytes}, want: "filename extension"},
		{name: "wrong media type", src: Source{Filename: "logo.png", ContentType: "image/jpeg", Content: pngBytes}, want: "content type must be image/png"},
		{name: "missing media type", src: Source{Filename: "logo.png", Content: pngBytes}, want: "content type must be image/png"},
		{name: "dimension", src: Source{Filename: "logo.png", ContentType: "image/png", Content: pngHeader(4097, 1)}, want: "dimensions must not exceed"},
		{name: "pixel count", src: Source{Filename: "logo.png", ContentType: "image/png", Content: pngHeader(4001, 4000)}, want: "total pixels"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			_, err := Validate(tt.src)
			r.ErrorContains(err, tt.want)
		})
	}
}

func testWebPBytes(t *testing.T) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	require.New(t).NoError(err)
	return content
}

func truncatedRasterAfterConfig(t *testing.T, content []byte) []byte {
	t.Helper()
	for cut := 1; cut < len(content); cut++ {
		candidate := content[:len(content)-cut]
		if _, _, configErr := image.DecodeConfig(bytes.NewReader(candidate)); configErr != nil {
			continue
		}
		if _, _, decodeErr := image.Decode(bytes.NewReader(candidate)); decodeErr != nil {
			return candidate
		}
	}
	require.New(t).FailNow("fixture has no header-valid truncated form")
	return nil
}

func encodeTestImage(t *testing.T, format string) []byte {
	t.Helper()
	r := require.New(t)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	switch format {
	case "gif":
		r.NoError(gif.Encode(&out, img, nil))
	case "jpeg":
		r.NoError(jpeg.Encode(&out, img, nil))
	default:
		r.NoError(png.Encode(&out, img))
	}
	return out.Bytes()
}

func pngHeader(width, height uint32) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8], data[9], data[10], data[11], data[12] = 8, 6, 0, 0, 0
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	out.Write(length)
	out.WriteString("IHDR")
	out.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(data)
	checksum := make([]byte, 4)
	binary.BigEndian.PutUint32(checksum, crc.Sum32())
	out.Write(checksum)
	return out.Bytes()
}
