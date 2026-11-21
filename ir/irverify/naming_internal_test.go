// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
		"type/pet": &ir.Model{ID: "type/pet", Name: named("PetStore"), Properties: []ir.Property{
			{ID: "property/pet/a", Name: named("userID")},
			{ID: "property/pet/b", Name: named("HTTPServer2FA")},
			{ID: "property/pet/c", Name: named("already_canonical")},
			{ID: "property/pet/d", Name: named("com.example.User")},
		}},
		"type/anonymous":     &ir.Model{ID: "type/anonymous", Name: ir.Naming{Hint: "anon_pet"}},
		"type/aliased":       &ir.Model{ID: "type/aliased", Name: aliased},
		"type/primitive/str": &ir.Primitive{ID: "type/primitive/str", PrimitiveKind: ir.PrimitiveString},
	}}
	check := checkNaming(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every name is neutral and derived, and the primitive exempt")
}

// TestNameExempt_IsTheOptionalNodesOwnNameAlone holds the exemption to the one
// Naming a listed node is named by. No ir type tells the parts apart, since a
// primitive holds no Naming but its name, so the node here is the test's own:
// one carrying a second Naming, which is not its name, and a node of its own
// inside it, held beside an unlisted node. No Name but the listed node's is
// exempt.
func TestNameExempt_IsTheOptionalNodesOwnNameAlone(t *testing.T) {
	t.Parallel()
	type inner struct{ Name ir.Naming }
	type twoNamings struct {
		Name, Other ir.Naming
		Inner       inner
	}
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
		"document.Listed.Name":       true,
		"document.Listed.Other":      false,
		"document.Listed.Inner.Name": false,
		"document.Unlisted.Name":     false,
	}, got)
}

// TestCheckNaming_ExemptsAPrimitiveInAFieldNotOneEmbedded holds the exemption
// to the field a Naming is reached through, as its path spells it: a primitive
// held in a field of any name keeps its own Name exempt, while a struct
// embedding one beside a Name of its own spells the primitive's Name, which
// that Name shadows, on itself, so neither is exempt, though both share a
// path. An exemption recorded by path silenced both there.
func TestCheckNaming_ExemptsAPrimitiveInAFieldNotOneEmbedded(t *testing.T) {
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
		{"embedded beside a Name", embedding{}, []string{"document.Name", "document.Name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			check := checkNaming(nil)
			ir.WalkValues(tc.root, ir.DocumentPath, check)
			var absent []string
			for _, violation := range check.report(declarations{}) {
				if violation.Code == "ir/naming-absent" {
					absent = append(absent, violation.Path)
				}
			}
			assert.Equal(t, tc.want, absent)
		})
	}
}
