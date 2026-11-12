// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestEntityRef_HasAFieldPerIdentityClass holds ir.EntityRef to the classes of
// identity: one field of each, and none of any other type, so an entity of a
// class the IR gains can be named the day the class lands.
func TestEntityRef_HasAFieldPerIdentityClass(t *testing.T) {
	t.Parallel()
	fields := map[reflect.Type]int{}
	for f := range reflect.TypeFor[ir.EntityRef]().Fields() {
		fields[f.Type]++
		assert.True(t, idTypes[f.Type], "EntityRef.%s is of %s, which is no identity class", f.Name, f.Type)
	}
	for class := range idTypes {
		assert.Equal(t, 1, fields[class], "EntityRef names an entity of class %s by exactly one field", class)
	}
}

// TestReplacementCarriers_AreEveryOneTheIRDeclares holds replacementCarriers to
// the ir sources: every struct type declaring an ID of an identity class beside
// a Deprecation is listed, and no other, so a carrier the IR gains is held the
// day it lands.
func TestReplacementCarriers_AreEveryOneTheIRDeclares(t *testing.T) {
	t.Parallel()
	classes := map[string]bool{}
	for class := range idTypes {
		classes[class.Name()] = true
	}
	fields := map[string]map[string]string{}
	for _, f := range declaredFields(t) {
		if fields[f.owner] == nil {
			fields[f.owner] = map[string]string{}
		}
		fields[f.owner][f.field.Name()] = types.TypeString(f.field.Type(), func(*types.Package) string { return "" })
	}
	var declared []string
	for owner, fs := range fields {
		if classes[fs["ID"]] && fs["Deprecation"] == "*Deprecation" {
			declared = append(declared, owner)
		}
	}
	listed := make([]string, 0, len(replacementCarriers))
	for carrier := range replacementCarriers {
		listed = append(listed, carrier.Name())
	}
	assert.ElementsMatch(t, declared, listed)
}
