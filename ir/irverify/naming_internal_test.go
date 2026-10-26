package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckNaming_VisitingCostsNoAllocation holds the check's cost for a Naming
// whose canonical the grammar derives from its source at nothing, a source
// already canonical or not: it compares the two as it reads the source, where
// rendering the canonical to compare costs a string, or one per word before
// that. A Primitive is left out, as its exemption is recorded by path. It is
// not run in parallel: AllocsPerRun measures the process.
func TestCheckNaming_VisitingCostsNoAllocation(t *testing.T) {
	named := func(source string) ir.Naming {
		return ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/pet": &ir.Model{ID: "t/pet", Name: named("PetStore"), Properties: []ir.Property{
			{ID: "p/pet/a", Name: named("userID")},
			{ID: "p/pet/b", Name: named("HTTPServer2FA")},
			{ID: "p/pet/c", Name: named("already_canonical")},
			{ID: "p/pet/d", Name: named("com.example.User")},
		}},
		"t/anon": &ir.Model{ID: "t/anon", Name: ir.Naming{Hint: "anon_pet"}},
	}}
	check := checkNaming(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every name is neutral and derived")
}
