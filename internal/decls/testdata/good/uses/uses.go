// Package uses imports fixture, so a load of both can show that they share one
// type universe.
package uses

import "github.com/dexpace/morphic/internal/decls/testdata/good/fixture"

// Favourite is a fixture.Color from the importer's side.
var Favourite = fixture.Red

// Pick names fixture.Red in its body.
func Pick() fixture.Color { return fixture.Red }

// Picker picks.
type Picker struct{}

// Pick names fixture.Red too, as a method.
func (*Picker) Pick() fixture.Color { return fixture.Red }

// Choose names fixture.Red after Pick does, so a list of the two in the order
// the file declares them is not sorted.
func Choose() fixture.Color { return fixture.Red }

// Other names no fixture constant.
func Other() fixture.Color { return Favourite }
