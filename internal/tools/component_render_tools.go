package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/render"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const RenderComponentImageToolName = "render_component_image"

const (
	renderDefaultAxialWidth    = 400
	renderDefaultAxialHeight   = 160
	renderDefaultUprightWidth  = 200
	renderDefaultUprightHeight = 300

	// renderDefaultAxialLabeledHeight and renderDefaultUprightLabeledHeight
	// apply instead of the plain defaults above when show_label is set and
	// the caller does not also supply an explicit height, so the value
	// line and caption have room instead of being cramped into the
	// unlabeled default canvas.
	renderDefaultAxialLabeledHeight   = 300
	renderDefaultUprightLabeledHeight = 320
)

// RenderComponentImageInput is the shared contract for every deterministic
// component template. Family selects which one of Resistor, Diode, LED,
// Capacitor, or Fuse is used; exactly the matching field must be supplied.
type RenderComponentImageInput struct {
	Family             render.Family         `json:"family" jsonschema:"Component family. Selects which of the family-specific parameter objects is required. Do not substitute the closest of these five for an unsupported component (a MOSFET, IC, connector, USB part, or anything else) — none of them represent those packages."`
	Orientation        render.Orientation    `json:"orientation,omitempty" jsonschema:"horizontal (default, each template's native layout) or vertical (the fully rendered glyph rotated 90 degrees clockwise)."`
	Background         render.Background     `json:"background,omitempty" jsonschema:"transparent (default), white, or color."`
	BackgroundColorHex string                `json:"background_color_hex,omitempty" jsonschema:"6-digit RGB hex color, for example #336699. Required when background is color, invalid otherwise."`
	Width              int                   `json:"width,omitempty" jsonschema:"Output width in pixels, 64-1024. Defaults to a family-appropriate preset."`
	Height             int                   `json:"height,omitempty" jsonschema:"Output height in pixels, 64-1024. Defaults to a family-appropriate preset."`
	Resistor           *RenderResistorInput  `json:"resistor,omitempty" jsonschema:"Required and only valid when family is resistor."`
	Diode              *RenderDiodeInput     `json:"diode,omitempty" jsonschema:"Required and only valid when family is diode."`
	LED                *RenderLEDInput       `json:"led,omitempty" jsonschema:"Required and only valid when family is led."`
	Capacitor          *RenderCapacitorInput `json:"capacitor,omitempty" jsonschema:"Required and only valid when family is capacitor."`
	Fuse               *RenderFuseInput      `json:"fuse,omitempty" jsonschema:"Required and only valid when family is fuse."`
}

// RenderResistorInput mirrors render.ResistorParams. Band colors are never
// supplied directly; they are derived deterministically from
// resistance_ohms, band_count, and tolerance_label using the IEC 60062
// color code.
type RenderResistorInput struct {
	ResistanceOhms float64               `json:"resistance_ohms" jsonschema:"Resistance in ohms. Must be exactly representable as (band_count - 1) significant digits times a power of ten, the same constraint real color-coded resistors satisfy."`
	BandCount      int                   `json:"band_count,omitempty" jsonschema:"4 (default) or 5."`
	ToleranceLabel render.ToleranceLabel `json:"tolerance_label" jsonschema:"Resistor tolerance."`
	Size           render.BodySize       `json:"size,omitempty" jsonschema:"Body size preset (medium is the default). Illustrative layout only, not a physical scale claim unless body_length_mm/body_diameter_mm are supplied."`
	BodyLengthMM   float64               `json:"body_length_mm,omitempty" jsonschema:"Optional body length in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with body_diameter_mm; sets the drawn aspect ratio only."`
	BodyDiameterMM float64               `json:"body_diameter_mm,omitempty" jsonschema:"Optional body diameter in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with body_length_mm."`
	Type           render.ResistorType   `json:"type,omitempty" jsonschema:"carbon_film is the default. Sets the default body color (beige or blue, matching near-universal manufacturer convention) and the show_label caption's type line; body_color_hex overrides the color when supplied. Wirewound and similar constructions are out of scope: they are not normally marked with painted color bands at all."`
	BodyColorHex   string                `json:"body_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard body color; otherwise omit it and let type pick the standard beige/blue default. Overrides type's default body color when supplied."`
	// PowerRatingWatts is intentionally not annotated with omitempty's
	// implicit "optional" phrasing alone: its jsonschema description below
	// spells out the size-preset side effect so a caller understands why
	// supplying it can change the drawn proportions even though size is
	// still nominally optional.
	PowerRatingWatts float64 `json:"power_rating_watts,omitempty" jsonschema:"Optional power rating in watts, > 0. Used in the show_label caption and, when size is not also explicitly supplied, to pick a larger body size preset for higher power ratings (a real convention, not a physical scale claim). Only include this when the caller explicitly stated a wattage; do not infer or estimate one."`
	ShowLabel        bool    `json:"show_label,omitempty" jsonschema:"Draw a bold resistance-and-tolerance line above the body (for example \"4.7 kΩ ±5%\") and a caption below it with the type, power rating when supplied, band count, and band color names. Every value is already present in the request or derived from it; nothing about material or package is invented."`
}

// RenderDiodeInput mirrors render.DiodeParams.
type RenderDiodeInput struct {
	BodyColorHex        string          `json:"body_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard body color; otherwise omit it and let the default black apply."`
	CathodeBandColorHex string          `json:"cathode_band_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard band color; otherwise omit it and let the default white apply. Must differ from body_color_hex."`
	CathodeSide         render.Side     `json:"cathode_side,omitempty" jsonschema:"left is the default."`
	Markings            string          `json:"markings,omitempty" jsonschema:"Optional body text, for example a part number. Only supply this when the caller gave an actual marking; do not invent one. Printable ASCII, 16 characters or fewer."`
	Size                render.BodySize `json:"size,omitempty" jsonschema:"Body size preset (medium is the default)."`
	BodyLengthMM        float64         `json:"body_length_mm,omitempty" jsonschema:"Optional body length in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with body_diameter_mm."`
	BodyDiameterMM      float64         `json:"body_diameter_mm,omitempty" jsonschema:"Optional body diameter in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with body_length_mm."`
}

// RenderLEDInput mirrors render.LEDParams.
type RenderLEDInput struct {
	LensColor   render.LEDLensColor `json:"lens_color" jsonschema:"LED lens color."`
	Diffused    bool                `json:"diffused,omitempty" jsonschema:"true for a matte diffused lens, false (default) for a clear/water-clear lens with a specular highlight."`
	CathodeSide render.Side         `json:"cathode_side,omitempty" jsonschema:"left is the default. The side of the chamfered polarity corner and the \"+\" anode mark."`
	Size        render.LEDSize      `json:"size,omitempty" jsonschema:"5mm is the default."`
	ShowLabel   bool                `json:"show_label,omitempty" jsonschema:"Draw a caption below the LED with its size, lens color, and lens finish (diffused or clear). Every value is already present in the request; nothing about brightness, forward voltage, or viewing angle is invented."`
}

// RenderCapacitorInput mirrors render.CapacitorParams.
type RenderCapacitorInput struct {
	BodyColorHex           string          `json:"body_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard body color; otherwise omit it and let the default black apply."`
	NegativeStripeColorHex string          `json:"negative_stripe_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard stripe color; otherwise omit it and let the default light grey apply. Must differ from body_color_hex."`
	CapacitanceMicrofarads float64         `json:"capacitance_microfarads,omitempty" jsonschema:"Optional capacitance in microfarads, > 0. Formatted into the primary marking line. Only include this when the caller stated a concrete capacitance; do not infer or estimate one."`
	VoltageRatingV         float64         `json:"voltage_rating_v,omitempty" jsonschema:"Optional voltage rating in volts, > 0. Requires capacitance_microfarads. Only include this when the caller stated a concrete voltage rating; do not infer or estimate one."`
	MarkingsText           string          `json:"markings_text,omitempty" jsonschema:"Optional extra marking line, for example a series code. Only supply this when the caller gave an actual marking; do not invent one. Printable ASCII, 16 characters or fewer."`
	NegativeSide           render.Side     `json:"negative_side,omitempty" jsonschema:"left is the default. The polarity stripe side."`
	Size                   render.BodySize `json:"size,omitempty" jsonschema:"Body size preset (medium is the default)."`
	DiameterMM             float64         `json:"diameter_mm,omitempty" jsonschema:"Optional body diameter in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with height_mm."`
	HeightMM               float64         `json:"height_mm,omitempty" jsonschema:"Optional body height in millimeters. Only supply this when the caller stated an actual physical dimension; do not estimate one. Must be supplied together with diameter_mm."`
}

// RenderFuseInput mirrors render.FuseParams.
type RenderFuseInput struct {
	RatingAmps    float64          `json:"rating_amps" jsonschema:"Fuse current rating in amps, > 0."`
	RatingVoltage float64          `json:"rating_voltage,omitempty" jsonschema:"Optional voltage rating in volts, > 0. Only include this when the caller stated a concrete voltage rating; do not infer or estimate one."`
	Speed         render.FuseSpeed `json:"speed,omitempty" jsonschema:"fast is the default. Also selects a straight versus coiled fusible element (slow is time-delay)."`
	CapColorHex   string           `json:"cap_color_hex,omitempty" jsonschema:"6-digit RGB hex color. Only supply this when the caller stated an actual non-standard cap color; otherwise omit it and let the default silver metal tone apply."`
	Size          render.FuseSize  `json:"size,omitempty" jsonschema:"5x20mm is the default. Aspect-ratio preset only, not a physical scale claim."`
}

// RenderComponentImageOutput is the bounded, deterministic PNG result.
type RenderComponentImageOutput struct {
	Status      string `json:"status"`
	Family      string `json:"family"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	Base64      string `json:"base64"`
}

// stringEnumSchema builds a JSON Schema string enum from an ordered list of
// values, as returned by one of internal/render's fixed-order accessor
// functions (for example render.ToleranceLabels()), so the schema and the
// package's own runtime validation cannot drift apart.
func stringEnumSchema(values []string) *jsonschema.Schema {
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	return &jsonschema.Schema{Type: "string", Enum: enum}
}

// componentRenderInputSchema builds render_component_image's input schema
// explicitly rather than relying on automatic inference, so every field
// with a fixed set of accepted values is registered with a real JSON Schema
// enum instead of only descriptive text.
func componentRenderInputSchema() (*jsonschema.Schema, error) {
	familyValues := make([]string, 0, len(render.Families()))
	for _, f := range render.Families() {
		familyValues = append(familyValues, string(f))
	}
	return jsonschema.For[RenderComponentImageInput](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[render.Family]():         stringEnumSchema(familyValues),
			reflect.TypeFor[render.Orientation]():    stringEnumSchema(render.Orientations()),
			reflect.TypeFor[render.Background]():     stringEnumSchema(render.Backgrounds()),
			reflect.TypeFor[render.ToleranceLabel](): stringEnumSchema(render.ToleranceLabels()),
			reflect.TypeFor[render.BodySize]():       stringEnumSchema(render.BodySizes()),
			reflect.TypeFor[render.ResistorType]():   stringEnumSchema(render.ResistorTypes()),
			reflect.TypeFor[render.Side]():           stringEnumSchema(render.Sides()),
			reflect.TypeFor[render.LEDLensColor]():   stringEnumSchema(render.LEDLensColors()),
			reflect.TypeFor[render.LEDSize]():        stringEnumSchema(render.LEDSizes()),
			reflect.TypeFor[render.FuseSpeed]():      stringEnumSchema(render.FuseSpeeds()),
			reflect.TypeFor[render.FuseSize]():       stringEnumSchema(render.FuseSizes()),
		},
	})
}

func registerComponentRenderTool(server *mcp.Server, deps Dependencies) {
	tool := ToolDescriptor(RenderComponentImageToolName, "Render component image",
		"Renders a deterministic PNG image for a common, highly repetitive electronic component (axial resistor, axial diode, through-hole LED, radial electrolytic capacitor, or glass fuse) from a small parameter set. Output is illustrative inventory imagery, not a datasheet or a claim of physical scale beyond explicitly supplied dimensions. Only these five families are supported: do not call this tool for a MOSFET, IC, connector, USB part, or any other component by approximating it with the closest of the five — none of them represent those packages. This tool does not upload or assign the image to InvenTree; pass the returned bytes to an attachment or primary-image tool for that.")
	schema, err := componentRenderInputSchema()
	if err != nil {
		panic(fmt.Errorf("%s: building input schema: %w", RenderComponentImageToolName, err))
	}
	tool.InputSchema = schema
	mcp.AddTool(server, tool, GuardTool(deps, RenderComponentImageToolName, renderComponentImage))
}

func renderComponentImage(ctx context.Context, _ *mcp.CallToolRequest, input RenderComponentImageInput) (*mcp.CallToolResult, RenderComponentImageOutput, error) {
	logger := logging.FromContext(ctx).With(slog.String("tool", RenderComponentImageToolName))
	ctx = logging.WithLogger(ctx, logger)
	logger.DebugContext(ctx, "tool called", slog.String("family", string(input.Family)))

	canvas, err := resolveRenderCanvas(input)
	if err != nil {
		return nil, RenderComponentImageOutput{}, err
	}

	var result render.Result
	switch input.Family {
	case render.FamilyResistor:
		result, err = renderResistorFamily(canvas, input)
	case render.FamilyDiode:
		result, err = renderDiodeFamily(canvas, input)
	case render.FamilyLED:
		result, err = renderLEDFamily(canvas, input)
	case render.FamilyCapacitor:
		result, err = renderCapacitorFamily(canvas, input)
	case render.FamilyFuse:
		result, err = renderFuseFamily(canvas, input)
	default:
		return nil, RenderComponentImageOutput{}, fmt.Errorf("family must be one of %v", render.Families())
	}
	if err != nil {
		return nil, RenderComponentImageOutput{}, err
	}

	out := RenderComponentImageOutput{
		Status:      StatusOK,
		Family:      string(input.Family),
		ContentType: "image/png",
		Width:       result.Width,
		Height:      result.Height,
		SHA256:      result.SHA256,
		Base64:      base64.StdEncoding.EncodeToString(result.PNG),
	}
	return nil, out, nil
}

func resolveRenderCanvas(input RenderComponentImageInput) (render.CanvasOptions, error) {
	defaultW, defaultH := renderDefaultAxialWidth, renderDefaultAxialHeight
	showLabel := (input.Resistor != nil && input.Resistor.ShowLabel) || (input.LED != nil && input.LED.ShowLabel)
	switch input.Family {
	case render.FamilyLED, render.FamilyCapacitor:
		defaultW, defaultH = renderDefaultUprightWidth, renderDefaultUprightHeight
		if showLabel {
			defaultH = renderDefaultUprightLabeledHeight
		}
	case render.FamilyResistor:
		if showLabel {
			defaultH = renderDefaultAxialLabeledHeight
		}
	}
	width, height := input.Width, input.Height
	if width == 0 {
		width = defaultW
	}
	if height == 0 {
		height = defaultH
	}

	orientation := input.Orientation
	if orientation == "" {
		orientation = render.OrientationHorizontal
	}
	background := input.Background
	if background == "" {
		background = render.BackgroundTransparent
	}

	canvas := render.CanvasOptions{
		Width:              width,
		Height:             height,
		Orientation:        orientation,
		Background:         background,
		BackgroundColorHex: input.BackgroundColorHex,
	}
	if err := render.ValidateCanvasOptions(canvas); err != nil {
		return render.CanvasOptions{}, err
	}
	return canvas, nil
}

func renderResistorFamily(canvas render.CanvasOptions, input RenderComponentImageInput) (render.Result, error) {
	if input.Resistor == nil {
		return render.Result{}, errors.New("resistor parameters are required when family is resistor")
	}
	if err := requireOnlyFamily(input, render.FamilyResistor); err != nil {
		return render.Result{}, err
	}
	p := input.Resistor
	return render.RenderResistor(canvas, render.ResistorParams{
		ResistanceOhms:   p.ResistanceOhms,
		BandCount:        p.BandCount,
		ToleranceLabel:   string(p.ToleranceLabel),
		Size:             string(p.Size),
		BodyLengthMM:     p.BodyLengthMM,
		BodyDiameterMM:   p.BodyDiameterMM,
		Type:             string(p.Type),
		BodyColorHex:     p.BodyColorHex,
		PowerRatingWatts: p.PowerRatingWatts,
		ShowLabel:        p.ShowLabel,
	})
}

func renderDiodeFamily(canvas render.CanvasOptions, input RenderComponentImageInput) (render.Result, error) {
	if input.Diode == nil {
		return render.Result{}, errors.New("diode parameters are required when family is diode")
	}
	if err := requireOnlyFamily(input, render.FamilyDiode); err != nil {
		return render.Result{}, err
	}
	p := input.Diode
	return render.RenderDiode(canvas, render.DiodeParams{
		BodyColorHex:        p.BodyColorHex,
		CathodeBandColorHex: p.CathodeBandColorHex,
		CathodeSide:         string(p.CathodeSide),
		Markings:            p.Markings,
		Size:                string(p.Size),
		BodyLengthMM:        p.BodyLengthMM,
		BodyDiameterMM:      p.BodyDiameterMM,
	})
}

func renderLEDFamily(canvas render.CanvasOptions, input RenderComponentImageInput) (render.Result, error) {
	if input.LED == nil {
		return render.Result{}, errors.New("led parameters are required when family is led")
	}
	if err := requireOnlyFamily(input, render.FamilyLED); err != nil {
		return render.Result{}, err
	}
	p := input.LED
	return render.RenderLED(canvas, render.LEDParams{
		LensColor:   string(p.LensColor),
		Diffused:    p.Diffused,
		CathodeSide: string(p.CathodeSide),
		Size:        string(p.Size),
		ShowLabel:   p.ShowLabel,
	})
}

func renderCapacitorFamily(canvas render.CanvasOptions, input RenderComponentImageInput) (render.Result, error) {
	if input.Capacitor == nil {
		return render.Result{}, errors.New("capacitor parameters are required when family is capacitor")
	}
	if err := requireOnlyFamily(input, render.FamilyCapacitor); err != nil {
		return render.Result{}, err
	}
	p := input.Capacitor
	return render.RenderCapacitor(canvas, render.CapacitorParams{
		BodyColorHex:           p.BodyColorHex,
		NegativeStripeColorHex: p.NegativeStripeColorHex,
		CapacitanceMicrofarads: p.CapacitanceMicrofarads,
		VoltageRatingV:         p.VoltageRatingV,
		MarkingsText:           p.MarkingsText,
		NegativeSide:           string(p.NegativeSide),
		Size:                   string(p.Size),
		DiameterMM:             p.DiameterMM,
		HeightMM:               p.HeightMM,
	})
}

func renderFuseFamily(canvas render.CanvasOptions, input RenderComponentImageInput) (render.Result, error) {
	if input.Fuse == nil {
		return render.Result{}, errors.New("fuse parameters are required when family is fuse")
	}
	if err := requireOnlyFamily(input, render.FamilyFuse); err != nil {
		return render.Result{}, err
	}
	p := input.Fuse
	return render.RenderFuse(canvas, render.FuseParams{
		RatingAmps:    p.RatingAmps,
		RatingVoltage: p.RatingVoltage,
		Speed:         string(p.Speed),
		CapColorHex:   p.CapColorHex,
		Size:          string(p.Size),
	})
}

// requireOnlyFamily rejects a request that supplies more than one
// family-specific parameter object, so a caller cannot silently mix
// parameters from an unselected family.
func requireOnlyFamily(input RenderComponentImageInput, selected render.Family) error {
	supplied := []struct {
		family  render.Family
		present bool
	}{
		{render.FamilyResistor, input.Resistor != nil},
		{render.FamilyDiode, input.Diode != nil},
		{render.FamilyLED, input.LED != nil},
		{render.FamilyCapacitor, input.Capacitor != nil},
		{render.FamilyFuse, input.Fuse != nil},
	}
	for _, entry := range supplied {
		if entry.family != selected && entry.present {
			return fmt.Errorf("only the %s parameter object may be supplied when family is %s", entry.family, selected)
		}
	}
	return nil
}
