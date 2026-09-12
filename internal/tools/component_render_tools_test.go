package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/render"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComponentRenderInputSchemaEnums proves render_component_image's
// generated JSON Schema actually carries a real enum for every
// closed-vocabulary field, sourced from the same internal/render accessor
// functions the runtime validates against — a schema-generation mistake
// here (a missing or mistyped TypeSchemas entry) would otherwise silently
// fall back to no enum and go uncaught by every other test.
func TestComponentRenderInputSchemaEnums(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	schema, err := componentRenderInputSchema()
	r.NoError(err)

	stringValues := func(vs []string) []any {
		out := make([]any, len(vs))
		for i, v := range vs {
			out[i] = v
		}
		return out
	}
	familyValues := func() []any {
		families := render.Families()
		out := make([]any, len(families))
		for i, f := range families {
			out[i] = string(f)
		}
		return out
	}

	a.Equal(familyValues(), schema.Properties["family"].Enum)
	a.Equal(stringValues(render.Orientations()), schema.Properties["orientation"].Enum)
	a.Equal(stringValues(render.Backgrounds()), schema.Properties["background"].Enum)

	resistor := schema.Properties["resistor"]
	r.NotNil(resistor)
	a.Equal(stringValues(render.ToleranceLabels()), resistor.Properties["tolerance_label"].Enum)
	a.Equal(stringValues(render.BodySizes()), resistor.Properties["size"].Enum)
	a.Equal(stringValues(render.ResistorTypes()), resistor.Properties["type"].Enum)

	diode := schema.Properties["diode"]
	r.NotNil(diode)
	a.Equal(stringValues(render.Sides()), diode.Properties["cathode_side"].Enum)
	a.Equal(stringValues(render.BodySizes()), diode.Properties["size"].Enum)

	led := schema.Properties["led"]
	r.NotNil(led)
	a.Equal(stringValues(render.LEDLensColors()), led.Properties["lens_color"].Enum)
	a.Equal(stringValues(render.Sides()), led.Properties["cathode_side"].Enum)
	a.Equal(stringValues(render.LEDSizes()), led.Properties["size"].Enum)

	capacitor := schema.Properties["capacitor"]
	r.NotNil(capacitor)
	a.Equal(stringValues(render.Sides()), capacitor.Properties["negative_side"].Enum)
	a.Equal(stringValues(render.BodySizes()), capacitor.Properties["size"].Enum)

	fuse := schema.Properties["fuse"]
	r.NotNil(fuse)
	a.Equal(stringValues(render.FuseSpeeds()), fuse.Properties["speed"].Enum)
	a.Equal(stringValues(render.FuseSizes()), fuse.Properties["size"].Enum)
}

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

func TestRenderComponentImageShowLabelUsesTallerDefaultHeight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, resistorOut, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%", ShowLabel: true},
	})
	r.NoError(err)
	a.Equal(renderDefaultAxialWidth, resistorOut.Width)
	a.Equal(renderDefaultAxialLabeledHeight, resistorOut.Height)

	_, ledOut, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "led", LED: &RenderLEDInput{LensColor: "green", ShowLabel: true},
	})
	r.NoError(err)
	a.Equal(renderDefaultUprightWidth, ledOut.Width)
	a.Equal(renderDefaultUprightLabeledHeight, ledOut.Height)

	// An explicit height always wins over the show_label default, for
	// either family.
	_, resistorExplicit, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Height: 500, Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%", ShowLabel: true},
	})
	r.NoError(err)
	a.Equal(500, resistorExplicit.Height)
}

func TestRenderComponentImageResistorTypeAndWattage(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, defaultType, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%"},
	})
	r.NoError(err)

	_, metalFilm, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%", Type: "metal_film"},
	})
	r.NoError(err)
	a.NotEqual(defaultType.SHA256, metalFilm.SHA256, "metal_film's default blue body must render differently from carbon_film's default beige body")

	_, customColor, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%", Type: "metal_film", BodyColorHex: "#3a9a4a"},
	})
	r.NoError(err)
	a.NotEqual(metalFilm.SHA256, customColor.SHA256, "body_color_hex must override type's default color")

	_, withWattage, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%", ShowLabel: true, PowerRatingWatts: 1},
	})
	r.NoError(err)
	_, withoutWattage, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, RenderComponentImageInput{
		Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4700, ToleranceLabel: "5%", ShowLabel: true},
	})
	r.NoError(err)
	a.NotEqual(withWattage.SHA256, withoutWattage.SHA256, "power_rating_watts must be reflected in the show_label caption and/or body size")
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
		{"invalid resistor type", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%", Type: "wirewound"}}},
		{"negative resistor power rating", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%", PowerRatingWatts: -1}}},
		{"invalid resistor body color hex", RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%", BodyColorHex: "not-a-color"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := renderComponentImage(ctx, &mcp.CallToolRequest{}, c.input)
			require.Error(t, err)
		})
	}
}

// validRenderResistorInput is a minimal valid render input reused by every
// render_and_attach_component_image test below; the family/parameter
// contract itself is exercised by TestRenderComponentImageValidation above.
func validRenderResistorInput() RenderComponentImageInput {
	return RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 100, ToleranceLabel: "5%"}}
}

func TestRenderAndAttachComponentImageRequiresPositivePartID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 0},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part_id", output.Clarification.Field)
	a.Empty(output.Base64)
	a.False(fake.uploadedAttachment)
}

func TestRenderAndAttachComponentImageUploadsWithoutSettingPrimary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(42, output.PartID)
	a.Equal(90, output.AttachmentID)
	a.False(output.PrimarySet)
	a.Empty(output.Base64)
	a.NotEmpty(output.SHA256)
	a.True(fake.uploadedAttachment)
	a.Equal("part", fake.lastAttachmentCreate.ModelType)
	a.Equal(42, fake.lastAttachmentCreate.ModelID)
	a.Equal("image/png", fake.lastAttachmentCreate.ContentType)
	a.NotEmpty(fake.lastAttachmentCreate.Content)
	a.False(fake.setPartPrimaryImage)
}

func TestRenderAndAttachComponentImageSetsPrimaryWhenNoExistingImage(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42, SetPrimary: true},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.PrimarySet)
	a.True(fake.setPartPrimaryImage)
	a.Equal(42, fake.lastSetPartPrimaryImagePartID)
	a.Equal("image/png", fake.lastSetPartPrimaryImageInput.ContentType)
}

func TestRenderAndAttachComponentImageReplacingExistingPrimaryRequiresConfirm(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existingImage := "/media/part_images/old.png"
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 42, Image: &existingImage}}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42, SetPrimary: true},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("confirm", output.Clarification.Field)
	a.False(fake.uploadedAttachment)
	a.False(fake.setPartPrimaryImage)

	_, output, err = renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42, SetPrimary: true, Confirm: true},
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.PrimarySet)
	a.True(fake.uploadedAttachment)
	a.True(fake.setPartPrimaryImage)
}

func TestRenderAndAttachComponentImageValidationFailureNeverTouchesClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, _, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: RenderComponentImageInput{Family: "resistor", Resistor: &RenderResistorInput{ResistanceOhms: 4712, ToleranceLabel: "5%"}},
		Attach:                    RenderAttachInput{PartID: 42},
	})
	r.Error(err)
	a.False(fake.uploadedAttachment)
	a.False(fake.setPartPrimaryImage)
}

func TestRenderAndAttachComponentImageReusesExistingAttachmentWithMatchingFilename(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	result, err := renderFromComponentInput(validRenderResistorInput())
	r.NoError(err)
	existingFilename := fmt.Sprintf("render_resistor_%s.png", result.SHA256[:12])
	fake := &fakeMilestoneLookupClient{attachments: []inventree.Attachment{{PK: 77, ModelType: "part", ModelID: 42, Filename: existingFilename}}}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42},
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(77, output.AttachmentID)
	a.False(fake.uploadedAttachment, "a matching existing attachment must be reused, not duplicated")
}

func TestRenderAndAttachComponentImageGetPartFailurePropagatesError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{getPartErr: errors.New("part not found")}

	_, _, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 999, SetPrimary: true},
	})
	r.Error(err)
	r.False(fake.uploadedAttachment)
}

func TestRenderAndAttachComponentImageUploadFailurePropagatesError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{uploadAttachmentErr: errors.New("part not found")}

	_, _, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 999},
	})
	r.Error(err)
}

func TestRenderAndAttachComponentImageSetPrimaryFailureIsPartialFailureWithRecoveryPlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{setPartPrimaryImageErr: errors.New("upstream rejected")}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42, SetPrimary: true},
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	a.Equal(42, output.PartID)
	a.Equal(90, output.AttachmentID)
	a.False(output.PrimarySet)
	a.NotEmpty(output.RecoveryPlan)
	a.Contains(output.RecoveryPlan, "attachment_id 90")
	a.Contains(output.RecoveryPlan, "part_id 42")
	a.NotContains(output.RecoveryPlan, "confirm:true", "no existing primary image was being replaced, so a retry needs no confirm")
	a.True(fake.uploadedAttachment)
}

func TestRenderAndAttachComponentImageSetPrimaryFailureWhileReplacingMentionsConfirm(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existingImage := "/media/part_images/old.png"
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 42, Image: &existingImage}, setPartPrimaryImageErr: errors.New("upstream rejected")}

	_, output, err := renderAndAttachComponentImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42, SetPrimary: true, Confirm: true},
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	a.Contains(output.RecoveryPlan, "attachment_id 90")
	a.Contains(output.RecoveryPlan, "part_id 42")
	a.Contains(output.RecoveryPlan, "confirm:true", "retrying set_primary_image against a part with an existing primary image needs confirm:true again")
}

func TestRenderAndAttachComponentImageRequiresClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, _, err := renderAndAttachComponentImage(Dependencies{})(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42},
	})
	r.ErrorIs(err, ErrLookupClientUnavailable)
}

func TestRenderAndAttachComponentImageRejectsClientMissingInterface(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return struct{}{}, nil }}

	_, _, err := renderAndAttachComponentImage(deps)(ctx, &mcp.CallToolRequest{}, RenderAndAttachComponentImageInput{
		RenderComponentImageInput: validRenderResistorInput(),
		Attach:                    RenderAttachInput{PartID: 42},
	})
	r.ErrorIs(err, ErrLookupClientUnavailable)
}

// TestComponentRenderAndAttachInputSchemaEnums mirrors
// TestComponentRenderInputSchemaEnums's thoroughness for
// render_and_attach_component_image's schema, proving the promoted-embedding
// path (RenderAndAttachComponentImageInput embeds RenderComponentImageInput)
// carries every enum through intact, that attach is required only on this
// tool's schema, and that render_component_image's own schema is untouched
// by the embedding.
func TestComponentRenderAndAttachInputSchemaEnums(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	stringValues := func(vs []string) []any {
		out := make([]any, len(vs))
		for i, v := range vs {
			out[i] = v
		}
		return out
	}

	schema, err := componentRenderAndAttachInputSchema()
	r.NoError(err)
	a.Equal(stringValues(render.Orientations()), schema.Properties["orientation"].Enum)
	a.Equal(stringValues(render.Backgrounds()), schema.Properties["background"].Enum)
	resistor := schema.Properties["resistor"]
	r.NotNil(resistor)
	a.Equal(stringValues(render.ToleranceLabels()), resistor.Properties["tolerance_label"].Enum)
	led := schema.Properties["led"]
	r.NotNil(led)
	a.Equal(stringValues(render.LEDLensColors()), led.Properties["lens_color"].Enum)

	a.Contains(schema.Required, "attach")
	r.NotNil(schema.Properties["attach"])
	a.Contains(schema.Properties["attach"].Required, "part_id")

	plainSchema, err := componentRenderInputSchema()
	r.NoError(err)
	a.NotContains(plainSchema.Required, "attach")
	a.Nil(plainSchema.Properties["attach"])
}
