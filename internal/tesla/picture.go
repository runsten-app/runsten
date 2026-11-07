package tesla

import (
	"net/url"
	"strings"
)

// Tesla's API gives no picture of the car, but tells its model, its paint and its rims:
// Tesla's configurator renders a car from these, by option codes, at an image endpoint of
// its own (the "compositor"). It is not documented: the codes below are those it rendered
// when tried one by one (2026-11), any other answering 412. It renders the models as
// first sold: the 2023 Model 3 (Highland) and the 2025 Model Y (Juniper) as the ones
// before them, their new paints only where the older model had them.

// compositor is the endpoint's address. Its view is the front three quarters, its picture
// 16:9, the car on a transparent ground; size is its width, in pixels.
const compositor = "https://static-assets.tesla.com/configurator/compositor"

// pictureSize is the width asked; the picture relay asks its own.
const pictureSize = "1920"

// look is how the compositor renders a model: its code, the options it requires (a trim
// and an interior, without which it answers 412), its paints and rims by the names
// vehicle_config gives them, and the rims rendered when the car's are not among them.
// baseRims are the paints rendered on those rims only, any other answering 412.
type look struct {
	model, base, rims string
	paints, wheels    map[string]string
	baseRims          map[string]bool
}

// looks are the models' by car_type.
var looks = map[string]look{
	"modely": {
		model: "my", base: "$MTY09,$INPB0", rims: "$WY19B",
		paints: map[string]string{
			"SolidBlack": "$PBSB", "DeepBlue": "$PPSB", "DeepBlueMetallic": "$PPSB",
			"RedMulticoat": "$PPMR", "Red": "$PPMR", "MidnightSilver": "$PMNG", "MidnightSilverMetallic": "$PMNG",
			"PearlWhite": "$PPSW", "PearlWhiteMulticoat": "$PPSW", "Quicksilver": "$PN00", "StealthGrey": "$PN01",
			"MidnightCherryRed": "$PR00", "UltraRed": "$PR01",
		},
		// HelixV220Dark, the Juniper's 20-inch, is rendered as the dark 20-inch the older
		// Model Y had.
		wheels: map[string]string{
			"Gemini19": "$WY19B", "Apollo19": "$WY19B", "Induction20Black": "$WY20P", "HelixV220Dark": "$WY20P",
		},
	},
	"model3": {
		model: "m3", base: "$MT322,$IBB1", rims: "$W38B",
		paints: map[string]string{
			"ObsidianBlack": "$PMBL", "MetallicBlack": "$PMBL", "SolidBlack": "$PBSB", "DeepBlue": "$PPSB",
			"DeepBlueMetallic": "$PPSB", "RedMulticoat": "$PPMR", "Red": "$PPMR", "MidnightSilver": "$PMNG",
			"MidnightSilverMetallic": "$PMNG", "SilverMetallic": "$PMSS", "Silver": "$PMSS",
			"PearlWhite": "$PPSW", "PearlWhiteMulticoat": "$PPSW",
		},
		wheels: map[string]string{
			"Pinwheel18": "$W38B", "Pinwheel18CapKit": "$W40B", "PinwheelRefresh18": "$W40B", "Sport19": "$W40B",
			"Sportwheel19": "$W39B", "Stiletto19": "$W39B", "Aero18": "$W41B", "StilettoRefresh19": "$W41B",
		},
		// Obsidian Black and Silver: not with $W40B nor $W41B.
		baseRims: map[string]bool{"$PMBL": true, "$PMSS": true},
	},
	"models": modelS, "models2": modelS, "lychee": modelS,
	"modelx": modelX, "tamarind": modelX,
}

// The Model S and X render fewer paints, and one rim each.
var (
	modelS = look{
		model: "ms", base: "$MTS14,$IBC00", rims: "$WS90",
		paints: map[string]string{
			"SolidBlack": "$PBSB", "DeepBlue": "$PPSB", "DeepBlueMetallic": "$PPSB", "RedMulticoat": "$PPMR",
			"Red": "$PPMR", "MidnightSilver": "$PMNG", "MidnightSilverMetallic": "$PMNG",
			"PearlWhite": "$PPSW", "PearlWhiteMulticoat": "$PPSW",
		},
	}
	modelX = look{
		model: "mx", base: "$MTX13,$IBE00", rims: "$WX00",
		paints: map[string]string{
			"SolidBlack": "$PBSB", "DeepBlue": "$PPSB", "DeepBlueMetallic": "$PPSB", "RedMulticoat": "$PPMR",
			"Red": "$PPMR", "MidnightSilver": "$PMNG", "MidnightSilverMetallic": "$PMNG",
			"PearlWhite": "$PPSW", "PearlWhiteMulticoat": "$PPSW", "UltraRed": "$PR01",
		},
	}
)

// pictureURL gives the compositor's picture of the car, or nothing for a model it does
// not render or a paint it is not known to: a car in another colour than its own would be
// wrong, while rims the compositor lacks are drawn as the model's first.
func pictureURL(c *vehicleConfig) string {
	if c.CarType == nil || c.ExteriorColor == nil {
		return ""
	}
	l, ok := looks[strings.ToLower(*c.CarType)]
	if !ok {
		return ""
	}
	paint, ok := l.paints[strings.TrimSpace(*c.ExteriorColor)]
	if !ok {
		return ""
	}
	rims := l.rims
	if c.WheelType != nil && !l.baseRims[paint] {
		if w, ok := l.wheels[strings.TrimSpace(*c.WheelType)]; ok {
			rims = w
		}
	}
	q := url.Values{
		"context":  {"design_studio_2"},
		"model":    {l.model},
		"view":     {"FRONT34"},
		"size":     {pictureSize},
		"bkba_opt": {"1"},
		"options":  {l.base + "," + paint + "," + rims},
	}
	return compositor + "?" + q.Encode()
}
