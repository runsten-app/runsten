package main

import (
	"runsten/internal/bmw"
	"runsten/internal/pleos"
	"runsten/internal/polestar"
	"runsten/internal/tesla"
	"runsten/internal/volvo"
)

// privateFields are the fields of each source's responses that the readings' CSV file
// leaves out. A source missing here is left out whole.
func privateFields() map[string][]string {
	return map[string][]string{
		volvo.Source:    volvo.PrivateFields(),
		polestar.Source: polestar.PrivateFields(),
		tesla.Source:    tesla.PrivateFields(),
		bmw.Source:      bmw.PrivateFields(),
		pleos.Source:    pleos.PrivateFields(),
	}
}
