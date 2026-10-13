// Package uses imports fixture, so a load of both can show that they share one
// type universe.
package uses

import "github.com/dexpace/morphic/internal/decls/testdata/good/fixture"

// Favourite is a fixture.Color from the importer's side.
var Favourite = fixture.Red
