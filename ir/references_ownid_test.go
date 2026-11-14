// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"go/types"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestIDReferences_AKeyedClassIsNamedIDOnlyWhereDeclared holds the premise the
// rule "a registry node's own ID is no reference" rests on: it is read off the
// field's name, so a struct of this package may carry a field named ID of a
// class a registry is keyed by only where that field is the identity IDReader
// reads the struct's declaration from. A struct that held such a field and
// declared nothing would have a reference skipped as a declaration.
func TestIDReferences_AKeyedClassIsNamedIDOnlyWhereDeclared(t *testing.T) {
	t.Parallel()
	keyed := map[string]bool{}
	for class := range ir.KeyedClasses() {
		keyed[class.Name()] = true
	}
	require.NotEmpty(t, keyed)

	var carriers []string
	for tn := range irDecls(t).Objects[*types.TypeName]() {
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for field := range st.Fields() {
			named, isNamed := field.Type().(*types.Named)
			if field.Name() == "ID" && isNamed && keyed[named.Obj().Name()] {
				carriers = append(carriers, tn.Name())
			}
		}
	}
	slices.Sort(carriers)
	require.Equal(t, []string{"AuthScheme", "Channel", "Message", "Server", "TypeCommon"}, carriers,
		"a struct naming a keyed class ID must declare an identity, and be read as such below")

	var reader ir.IDReader
	for _, v := range []reflect.Value{
		reflect.ValueOf(&ir.AuthScheme{}).Elem(), reflect.ValueOf(&ir.Channel{}).Elem(),
		reflect.ValueOf(&ir.Message{}).Elem(), reflect.ValueOf(&ir.Server{}).Elem(),
		reflect.ValueOf(&ir.TypeCommon{}).Elem(),
	} {
		v.FieldByName("ID").SetString("sentinel")

		class, id, declares := reader.DeclaredIDOf(v)

		assert.True(t, declares, "%s", v.Type())
		assert.Equal(t, "sentinel", id, "%s: IDReader reads a different field", v.Type())
		assert.True(t, keyed[class.Name()], "%s declares an ID of class %s", v.Type(), class)
	}
}

// TestIDReferences_ARegistryNodesOwnIDIsNoReference walks a model filed under a
// key other than its ID, holding a reference to a type nothing declares. Of its
// strings only the key and that reference are kept to dangle: its own ID is its
// declaration, which irverify holds to the key and no resolution can answer
// for. The same node under its own key keeps the reference alone.
func TestIDReferences_ARegistryNodesOwnIDIsNoReference(t *testing.T) {
	t.Parallel()
	build := func(key ir.TypeID) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{
			key: &ir.Model{ID: "t/x/B", Base: &ir.TypeRef{Target: "t/x/C"}},
		}}
	}

	mismatched, _ := walkedRefs(build("t/x/A"))
	assert.Equal(t, []string{
		"doc.Types[t/x/A].Base.Target=t/x/C",
	}, spelled(mismatched.Kept()), "the model's own ID is no reference, and its key resolves to itself")

	sound, _ := walkedRefs(build("t/x/B"))
	assert.Equal(t, []string{"doc.Types[t/x/B].Base.Target=t/x/C"}, spelled(sound.Kept()))
}
