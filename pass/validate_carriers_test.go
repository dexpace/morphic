package pass_test

import (
	"go/types"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
)

// payloadType is the node whose carriers this file enumerates.
var payloadType = reflect.TypeFor[ir.Payload]()

// maxFieldTypeDepth bounds the walk through one field's type constructors (the
// bounded-recursion rule). A field type is a finite tower of pointers, slices
// and maps, except that `type T *T` is legal Go, which is what the counter is
// for; reaching it means the walk stopped being a walk over the IR, so it
// fails rather than truncating.
const maxFieldTypeDepth = 32

// TestEncodingCarriers_NameEveryPayloadFieldInTheIR fails when the IR declares a
// field carrying an ir.Payload that encodingCarriers does not name.
//
// forEachPayload names the Payload-bearing fields by hand, a coupling the
// compiler cannot check: a carrier added to the IR would otherwise be walked by
// neither the checks built on the walk nor the cases below, with the whole
// suite green. The guard holds both lists in two steps. This test holds
// encodingCarriers against the IR; TestValidate_EncodingKeyAddressesNoProperty
// holds forEachPayload against encodingCarriers by requiring a diagnostic from
// every entry.
func TestEncodingCarriers_NameEveryPayloadFieldInTheIR(t *testing.T) {
	t.Parallel()
	carriers := encodingCarriers()
	listed := make([]string, 0, len(carriers))
	for _, c := range carriers {
		listed = append(listed, c.field)
	}
	slices.Sort(listed)

	found := payloadFields(t)
	require.NotEmpty(t, found, "the shape holds no ir.Payload field at all, so the read reached "+
		"nothing and proves nothing about the ones encodingCarriers names")
	assert.Empty(t, cmp.Diff(found, listed),
		"encodingCarriers must name every ir field that carries an ir.Payload, once each "+
			"(-declared +listed); a new one also has to be walked by forEachPayload")
}

// payloadFields returns "Owner.Field", sorted, for every struct field the IR
// declares whose type is or contains an ir.Payload. It reads the fields off the
// JSON shape ir.Document reaches, which lists a sealed sum's variants from the
// ir sources, so a carrier is covered the day it is declared.
func payloadFields(t *testing.T) []string {
	t.Helper()
	p, err := decls.Load(payloadType.PkgPath())
	require.NoError(t, err)
	shape, err := jsonshape.Read(p, "Document")
	require.NoError(t, err)

	var found []string
	for _, typ := range shape.Types {
		for _, m := range typ.Members {
			if carriesPayload(t, m.Field.Type(), 0) {
				found = append(found, typ.Named.Obj().Name()+"."+m.Field.Name())
			}
		}
	}
	slices.Sort(found)
	return found
}

// carriesPayload reports whether a field's type is an ir.Payload, or a pointer,
// slice, array or map of one.
//
// It does not descend into structs. A field whose type merely reaches a Payload
// further down is the spine leading to a carrier — Document.Services and
// Document.Messages between them reach every one of today's — and naming the
// spine would name most of the IR.
func carriesPayload(t *testing.T, typ types.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxFieldTypeDepth, "resolving a field type exceeded %d steps", maxFieldTypeDepth)
	if named, isNamed := types.Unalias(typ).(*types.Named); isNamed {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == payloadType.PkgPath() && obj.Name() == payloadType.Name() {
			return true
		}
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Slice:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Array:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Map:
		return carriesPayload(t, u.Key(), depth+1) || carriesPayload(t, u.Elem(), depth+1)
	default:
		return false // a struct is a carrier's owner, not a carrier
	}
}
