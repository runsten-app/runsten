package core

import "testing"

func TestCellOf(t *testing.T) {
	for _, tt := range []struct {
		p    Position
		want GeoCell
	}{
		{Position{45.76404, 4.83566}, GeoCell{457640, 48357}},
		{Position{45.76405, 4.83565}, GeoCell{457641, 48357}}, // half away from zero
		{Position{-33.86882, 151.20929}, GeoCell{-338688, 1512093}},
		{Position{90, -180}, GeoCell{900000, -1800000}},
	} {
		if got := CellOf(tt.p); got != tt.want {
			t.Errorf("CellOf(%v) = %v, want %v", tt.p, got, tt.want)
		}
	}
	if c := (GeoCell{457640, 48357}).Center(); c != (Position{45.764, 4.8357}) {
		t.Errorf("center = %v", c)
	}
}
