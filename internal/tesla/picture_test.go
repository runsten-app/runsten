package tesla

import (
	"net/url"
	"testing"
)

func TestPictureURL(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name                  string
		carType, paint, wheel *string
		model, options        string // empty: no picture
	}{
		{"a Model Y", str("modely"), str("DeepBlue"), str("Induction20Black"), "my", "$MTY09,$INPB0,$PPSB,$WY20P"},
		{"a Model 3", str("Model3"), str("MidnightSilver"), str("Stiletto19"), "m3", "$MT322,$IBB1,$PMNG,$W39B"},
		{"a Model S", str("lychee"), str("PearlWhite"), str("Tempest19"), "ms", "$MTS14,$IBC00,$PPSW,$WS90"},
		{"a Model X", str("modelx"), str("UltraRed"), nil, "mx", "$MTX13,$IBE00,$PR01,$WX00"},
		{"rims not rendered", str("modely"), str("PearlWhite"), str("Photon19"), "my", "$MTY09,$INPB0,$PPSW,$WY19B"},
		{"no rims told", str("model3"), str("SolidBlack"), nil, "m3", "$MT322,$IBB1,$PBSB,$W38B"},
		{"a paint on the base rims only", str("model3"), str("ObsidianBlack"), str("Aero18"), "m3", "$MT322,$IBB1,$PMBL,$W38B"},
		{"a paint the model lacks", str("model3"), str("StealthGrey"), str("Aero18"), "", ""},
		{"an unknown paint", str("modely"), str("Rainbow"), nil, "", ""},
		{"no paint told", str("modely"), nil, nil, "", ""},
		{"a model not rendered", str("cybertruck"), str("SolidBlack"), nil, "", ""},
		{"no model told", nil, str("SolidBlack"), nil, "", ""},
	} {
		got := pictureURL(&vehicleConfig{CarType: tc.carType, ExteriorColor: tc.paint, WheelType: tc.wheel})
		if tc.model == "" {
			if got != "" {
				t.Errorf("%s: got %s, want none", tc.name, got)
			}
			continue
		}
		u, err := url.Parse(got)
		if err != nil || u.Scheme+"://"+u.Host+u.Path != compositor {
			t.Fatalf("%s: got %s, %v", tc.name, got, err)
		}
		q := u.Query()
		if q.Get("model") != tc.model || q.Get("options") != tc.options || q.Get("view") != "FRONT34" ||
			q.Get("size") != pictureSize || q.Get("bkba_opt") != "1" {
			t.Errorf("%s: got %s", tc.name, got)
		}
	}
}
