package render

//go:generate go run ../../cmd/generate-render-samples

// Sample is one named, reproducible example configuration for the
// component-image gallery. The same list drives both the checked-in
// gallery under docs/images/render-samples/ (via cmd/generate-render-samples)
// and TestRenderSamplesMatchCheckedInGallery's regression check, so a
// rendering change that alters pixel output is caught by that test instead
// of only being noticed by eye.
type Sample struct {
	Name        string
	Family      Family
	Description string
	Render      func() (Result, error)
}

func landscape(w, h int, orientation Orientation, background Background) CanvasOptions {
	return CanvasOptions{Width: w, Height: h, Orientation: orientation, Background: background}
}

// Samples is fixed and ordered; the gallery and its regression test both
// iterate it directly rather than a map, so ordering and content stay
// stable across generations.
var Samples = []Sample{
	{
		Name:        "resistor_carbon_film_100r",
		Family:      FamilyResistor,
		Description: "100 Ω, ±5% tolerance, default carbon film body, no caption.",
		Render: func() (Result, error) {
			return RenderResistor(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 100, ToleranceLabel: "5%"})
		},
	},
	{
		Name:        "resistor_carbon_film_4k7_label",
		Family:      FamilyResistor,
		Description: "4.7 kΩ, ±5%, carbon film, with show_label: the value line above and the type/wattage/band caption below.",
		Render: func() (Result, error) {
			return RenderResistor(landscape(360, 300, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 4700, ToleranceLabel: "5%", PowerRatingWatts: 0.25, ShowLabel: true})
		},
	},
	{
		Name:        "resistor_metal_film_499r_5band",
		Family:      FamilyResistor,
		Description: "499 Ω, ±1%, 5-band metal film (blue body is the default for this type).",
		Render: func() (Result, error) {
			return RenderResistor(landscape(360, 300, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 499, BandCount: 5, ToleranceLabel: "1%", Type: "metal_film", ShowLabel: true})
		},
	},
	{
		Name:        "resistor_sub_ohm_silver_multiplier",
		Family:      FamilyResistor,
		Description: "0.47 Ω, ±5%: a silver multiplier band and the shorthand-free \"0.47 Ω\" value line for a sub-1-ohm value.",
		Render: func() (Result, error) {
			return RenderResistor(landscape(360, 300, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 0.47, ToleranceLabel: "5%", ShowLabel: true})
		},
	},
	{
		Name:        "resistor_10m_1w_metal_film",
		Family:      FamilyResistor,
		Description: "10 MΩ, ±10%, metal film, 1 W: power_rating_watts with no explicit size picks the larger body preset.",
		Render: func() (Result, error) {
			return RenderResistor(landscape(360, 300, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 10_000_000, ToleranceLabel: "10%", Type: "metal_film", PowerRatingWatts: 1, ShowLabel: true})
		},
	},
	{
		Name:        "resistor_custom_body_color",
		Family:      FamilyResistor,
		Description: "220 Ω, ±5%: body_color_hex overrides the type-derived default color (green here, with carbon_film's caption label unaffected).",
		Render: func() (Result, error) {
			return RenderResistor(landscape(360, 300, OrientationHorizontal, BackgroundWhite),
				ResistorParams{ResistanceOhms: 220, ToleranceLabel: "5%", BodyColorHex: "#3a9a4a", ShowLabel: true})
		},
	},
	{
		Name:        "resistor_vertical_orientation",
		Family:      FamilyResistor,
		Description: "1 kΩ, ±5%, orientation: vertical — the fully rendered glyph (including the caption) rotated 90 degrees clockwise.",
		Render: func() (Result, error) {
			return RenderResistor(landscape(220, 380, OrientationVertical, BackgroundWhite),
				ResistorParams{ResistanceOhms: 1000, ToleranceLabel: "5%", ShowLabel: true})
		},
	},

	{
		Name:        "diode_1n4148_default",
		Family:      FamilyDiode,
		Description: "Default black body, white cathode band on the left, part-number markings.",
		Render: func() (Result, error) {
			return RenderDiode(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				DiodeParams{Markings: "1N4148"})
		},
	},
	{
		Name:        "diode_cathode_right",
		Family:      FamilyDiode,
		Description: "Same defaults with cathode_side: right, mirroring the band position and marking placement.",
		Render: func() (Result, error) {
			return RenderDiode(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				DiodeParams{Markings: "1N4007", CathodeSide: "right"})
		},
	},
	{
		Name:        "diode_custom_colors",
		Family:      FamilyDiode,
		Description: "Custom body_color_hex and cathode_band_color_hex.",
		Render: func() (Result, error) {
			return RenderDiode(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				DiodeParams{BodyColorHex: "#8a1010", CathodeBandColorHex: "#f0f0f0", Markings: "1N5819"})
		},
	},
	{
		Name:        "diode_no_markings",
		Family:      FamilyDiode,
		Description: "markings is optional; omitting it leaves a plain banded body.",
		Render: func() (Result, error) {
			return RenderDiode(landscape(320, 130, OrientationHorizontal, BackgroundWhite), DiodeParams{})
		},
	},

	{
		Name:        "led_red_clear_5mm",
		Family:      FamilyLED,
		Description: "Default 5mm size, red lens, clear (glossy) finish, cathode on the left.",
		Render: func() (Result, error) {
			return RenderLED(landscape(200, 260, OrientationHorizontal, BackgroundWhite),
				LEDParams{LensColor: "red"})
		},
	},
	{
		Name:        "led_green_diffused",
		Family:      FamilyLED,
		Description: "diffused: true gives a matte lens instead of the glossy specular highlight.",
		Render: func() (Result, error) {
			return RenderLED(landscape(200, 260, OrientationHorizontal, BackgroundWhite),
				LEDParams{LensColor: "green", Diffused: true})
		},
	},
	{
		Name:        "led_blue_3mm",
		Family:      FamilyLED,
		Description: "size: 3mm — a smaller illustrative preset than the 5mm default.",
		Render: func() (Result, error) {
			return RenderLED(landscape(200, 260, OrientationHorizontal, BackgroundWhite),
				LEDParams{LensColor: "blue", Size: "3mm"})
		},
	},
	{
		Name:        "led_yellow_cathode_right_10mm",
		Family:      FamilyLED,
		Description: "size: 10mm, cathode_side: right — the chamfered corner and \"+\" mark both mirror.",
		Render: func() (Result, error) {
			return RenderLED(landscape(200, 260, OrientationHorizontal, BackgroundWhite),
				LEDParams{LensColor: "yellow", Size: "10mm", CathodeSide: "right"})
		},
	},
	{
		Name:        "led_white_with_label",
		Family:      FamilyLED,
		Description: "show_label adds a caption with the size, lens color, and lens finish.",
		Render: func() (Result, error) {
			return RenderLED(landscape(220, 320, OrientationHorizontal, BackgroundWhite),
				LEDParams{LensColor: "white", ShowLabel: true})
		},
	},

	{
		Name:        "capacitor_100uf_16v",
		Family:      FamilyCapacitor,
		Description: "Default black body, negative stripe on the left, capacitance and voltage rating printed vertically along the can.",
		Render: func() (Result, error) {
			return RenderCapacitor(landscape(200, 300, OrientationHorizontal, BackgroundWhite),
				CapacitorParams{CapacitanceMicrofarads: 100, VoltageRatingV: 16})
		},
	},
	{
		Name:        "capacitor_1000uf_35v_blue_right",
		Family:      FamilyCapacitor,
		Description: "Custom body_color_hex, negative_side: right, and an extra markings_text line (a series code).",
		Render: func() (Result, error) {
			return RenderCapacitor(landscape(200, 300, OrientationHorizontal, BackgroundWhite),
				CapacitorParams{CapacitanceMicrofarads: 1000, VoltageRatingV: 35, BodyColorHex: "#1a5a8a", NegativeSide: "right", MarkingsText: "X7R"})
		},
	},
	{
		Name:        "capacitor_capacitance_only",
		Family:      FamilyCapacitor,
		Description: "voltage_rating_v is optional; omitting it prints capacitance alone.",
		Render: func() (Result, error) {
			return RenderCapacitor(landscape(200, 300, OrientationHorizontal, BackgroundWhite),
				CapacitorParams{CapacitanceMicrofarads: 47})
		},
	},
	{
		Name:        "capacitor_green_body",
		Family:      FamilyCapacitor,
		Description: "Another body_color_hex example, showing the cylindrical shading and top-cap disk on a lighter color.",
		Render: func() (Result, error) {
			return RenderCapacitor(landscape(200, 300, OrientationHorizontal, BackgroundWhite),
				CapacitorParams{CapacitanceMicrofarads: 220, VoltageRatingV: 25, BodyColorHex: "#2a6a3a"})
		},
	},

	{
		Name:        "fuse_2a_fast_horizontal",
		Family:      FamilyFuse,
		Description: "2 A, default fast speed (straight fusible element), default 5x20mm aspect preset.",
		Render: func() (Result, error) {
			return RenderFuse(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				FuseParams{RatingAmps: 2})
		},
	},
	{
		Name:        "fuse_1_5a_slow_horizontal",
		Family:      FamilyFuse,
		Description: "1.5 A, 250 V, speed: slow gives a coiled time-delay element; the rating is printed on an opaque backing panel so it stays legible over the coil.",
		Render: func() (Result, error) {
			return RenderFuse(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				FuseParams{RatingAmps: 1.5, RatingVoltage: 250, Speed: "slow"})
		},
	},
	{
		Name:        "fuse_500ma_6x30",
		Family:      FamilyFuse,
		Description: "500 mA (sub-1A ratings format in milliamps), size: 6x30mm aspect preset.",
		Render: func() (Result, error) {
			return RenderFuse(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				FuseParams{RatingAmps: 0.5, Size: "6x30mm"})
		},
	},
	{
		Name:        "fuse_3_15a_slow_vertical_transparent",
		Family:      FamilyFuse,
		Description: "3.15 A, slow, orientation: vertical, background: transparent.",
		Render: func() (Result, error) {
			return RenderFuse(landscape(160, 340, OrientationVertical, BackgroundTransparent),
				FuseParams{RatingAmps: 3.15, Speed: "slow"})
		},
	},
	{
		Name:        "fuse_custom_cap_color",
		Family:      FamilyFuse,
		Description: "cap_color_hex overrides the default silver metal end caps.",
		Render: func() (Result, error) {
			return RenderFuse(landscape(320, 130, OrientationHorizontal, BackgroundWhite),
				FuseParams{RatingAmps: 1, CapColorHex: "#c8a028"})
		},
	},
}
