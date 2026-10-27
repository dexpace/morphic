package irverify

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckNaming_VisitingCostsNoAllocation holds the check's cost for a Naming
// it has nothing to report on at nothing: a canonical the grammar derives from
// its source, compared as the source is read rather than rendered to compare; a
// primitive's empty name, exempt by the field the walk reached it through
// rather than by a path recorded for every primitive; and an alias list longer
// than a map holds without allocating, held through the one map the check
// keeps. It is not run in parallel: AllocsPerRun measures the process.
func TestCheckNaming_VisitingCostsNoAllocation(t *testing.T) {
	named := func(source string) ir.Naming {
		return ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
	}
	aliased := named("Aliased")
	for i := range 20 {
		aliased.Aliases = append(aliased.Aliases, "com.example.Aliased"+strconv.Itoa(i))
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/pet": &ir.Model{ID: "t/pet", Name: named("PetStore"), Properties: []ir.Property{
			{ID: "p/pet/a", Name: named("userID")},
			{ID: "p/pet/b", Name: named("HTTPServer2FA")},
			{ID: "p/pet/c", Name: named("already_canonical")},
			{ID: "p/pet/d", Name: named("com.example.User")},
		}},
		"t/anon":     &ir.Model{ID: "t/anon", Name: ir.Naming{Hint: "anon_pet"}},
		"t/aliased":  &ir.Model{ID: "t/aliased", Name: aliased},
		"t/prim/str": &ir.Primitive{ID: "t/prim/str", Prim: ir.PrimString},
	}}
	check := checkNaming(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every name is neutral and derived, and the primitive exempt")
}

// TestNameExempt_IsTheOptionalNodesOwnNameAlone holds the exemption to the one
// Naming a listed node is named by. No ir type tells the parts apart, since a
// primitive holds no Naming but its name, so the node here is the test's own:
// one carrying a second Naming, which is not its name, held inside an unlisted
// node whose Name is not exempt either.
func TestNameExempt_IsTheOptionalNodesOwnNameAlone(t *testing.T) {
	t.Parallel()
	type twoNamings struct{ Name, Other ir.Naming }
	type holder struct {
		Listed   twoNamings
		Unlisted struct{ Name ir.Naming }
	}
	optional := map[reflect.Type]bool{reflect.TypeFor[twoNamings](): true}
	got := map[string]bool{}
	ir.WalkValues(holder{}, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Type() == namingType {
			got[at.String()] = nameExempt(at, optional)
		}
		return true
	}))
	assert.Equal(t, map[string]bool{
		"doc.Listed.Name":   true,
		"doc.Listed.Other":  false,
		"doc.Unlisted.Name": false,
	}, got)
}

// TestCheckNaming_ExemptsAPrimitivesOwnNameWhereverItSits holds the exemption
// to the field a Naming is reached through, as its path spells it: a primitive
// held in a field of any name keeps its own Name exempt, and a struct embedding
// one does not inherit the exemption, though its own Name and the primitive's
// promoted one share a path. An exemption recorded by path silenced both there.
func TestCheckNaming_ExemptsAPrimitivesOwnNameWhereverItSits(t *testing.T) {
	t.Parallel()
	type held struct{ Kept ir.Primitive }
	type embedding struct {
		ir.Primitive
		Name ir.Naming
	}
	for _, tc := range []struct {
		name string
		root any
		want []string
	}{
		{"held in a field", held{}, nil},
		{"embedded beside a Name", embedding{}, []string{"doc.Name", "doc.Name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			check := checkNaming(nil)
			ir.WalkValues(tc.root, ir.DocumentPath, check)
			var absent []string
			for _, v := range check.report(declarations{}) {
				if v.Code == "ir/naming-absent" {
					absent = append(absent, v.Path)
				}
			}
			assert.Equal(t, tc.want, absent)
		})
	}
}
