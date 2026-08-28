package tools

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderComponentImageEachFamily(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input RenderComponentImageInput
	}{
		{"resistor", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%"}}},
		{"diode", RenderComponentImageInput{Family: "diode", Diode: &RenderDiodeInput{Markings: "1N4148"}}},
		{"led", RenderComponentImageInput{Family: "led", LED: &RenderLEDInput{LensColor: "red"}}},
		{"capacitor", RenderComponentImageInput{Family: "capacitor", Capacitor: &RenderCapacitorInput{CapacitanceMicrofarads: 100, VoltageRatingV: 16}}},
		{"fuse", RenderComponentImageInput{Family: "fuse", Fuse: &RenderFuseInput{RatingAmps: 1.5}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			result, output, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, c.input)
			r.NoError(err)
			r.Nil(result)
			a.Equal(StatusOK, output.Status)
			a.Equal(c.name, output.Family)
			a.Equal("image/png", output.ContentType)
			a.NotEmpty(output.SHA256)
			a.NotEmpty(output.Base64)

			raw, decodeErr := base64.StdEncoding.DecodeString(output.Base64)
			r.NoError(decodeErr)
			img, pngErr := png.Decode(bytes.NewReader(raw))
			r.NoError(pngErr)
			a.Equal(output.Width, img.Bounds().Dx())
			a.Equal(output.Height, img.Bounds().Dy())
		})
	}
}

func TestRenderComponentImageDefaultsDimensionsByFamily(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, resistorOut, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"},
	})
	r.NoError(err)
	a.Equal(renderDefaultAxialWidth, resistorOut.Width)
	a.Equal(renderDefaultAxialHeight, resistorOut.Height)

	_, ledOut, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "led", LED: &RenderLEDInput{LensColor: "green"},
	})
	r.NoError(err)
	a.Equal(renderDefaultUprightWidth, ledOut.Width)
	a.Equal(renderDefaultUprightHeight, ledOut.Height)
}

func TestRenderComponentImageValidation(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)

	cases := []struct {
		name  string
		input RenderComponentImageInput
	}{
		{"unknown family", RenderComponentImageInput{Family: "transistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}},
		{"missing parameter object", RenderComponentImageInput{Family: "resistor"}},
		{"mismatched parameter object", RenderComponentImageInput{Family: "resistor", Diode: &RenderDiodeInput{}}},
		{"both parameter objects", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}, Diode: &RenderDiodeInput{}}},
		{"invalid canvas background", RenderComponentImageInput{Family: "resistor", Background: "rainbow", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}},
		{"width out of bounds", RenderComponentImageInput{Family: "resistor", Width: 5, Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}},
		{"invalid resistor value", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4712, ToleranceLabel: "5%"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, c.input)
			require.Error(t, err)
		})
	}
}
