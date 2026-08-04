package ir_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestDocumentRegistries_DerivedFromDocumentShape pins the derivation rule that
// replaces a hand-written registry table in each checker: every ID-keyed map on
// Document is a registry, labelled by the field that declares it, and a map keyed
// by plain string — Unmodeled keys on a source construct's name, not an identity
// — is not.
func TestDocumentRegistries_DerivedFromDocumentShape(t *testing.T) {
	t.Parallel()
	regs := ir.DocumentRegistries(&ir.Document{})

	want := map[reflect.Type]string{
		reflect.TypeFor[ir.TypeID]():    "types",
		reflect.TypeFor[ir.ChannelID](): "channels",
		reflect.TypeFor[ir.MessageID](): "messages",
		reflect.TypeFor[ir.AuthID]():    "auth",
	}
	for idType, label := range want {
		reg, isRegistry := regs[idType]
		require.True(t, isRegistry, "%s names a Document registry", idType.Name())
		assert.Equal(t, label, reg.Label)
	}
	_, isRegistry := regs[reflect.TypeFor[string]()]
	assert.False(t, isRegistry, "a plain string key is a name, not an identity")
	assert.Len(t, regs, len(want), "Document declares exactly these ID-keyed registries")
}

// TestRegistry_HasResolvesOnlyDeclaredIDs drives resolution both ways: an ID the
// registry declares resolves, one it does not declare dangles.
func TestRegistry_HasResolvesOnlyDeclaredIDs(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/x/M": &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/M"}},
	}}

	types := ir.DocumentRegistries(doc)[reflect.TypeFor[ir.TypeID]()]
	assert.True(t, types.Has("t/x/M"))
	assert.False(t, types.Has("t/x/Ghost"))
}

// TestRegistry_ZeroValueDeclaresNothing holds the report-only guard: a lookup for
// an ID class the document registers no map for yields the zero Registry, and
// asking it beats indexing an invalid reflect.Value and panicking. No site a
// checker collects reaches this, since the classes it collects come from the same
// derivation — which is exactly why it is asserted here rather than left to
// chance.
func TestRegistry_ZeroValueDeclaresNothing(t *testing.T) {
	t.Parallel()
	assert.False(t, ir.Registry{}.Has("op/x"))
	assert.Empty(t, ir.DocumentRegistries(nil), "a nil document declares no registry at all")
}

// TestRefNoun_NamesTheReferenceClass pins the singular both checkers spell a
// dangling-reference code with, so one defect reads under one code whichever of
// them reports it. PropID is included though it addresses no registry: the noun
// is derived from the type, and pass.Validate reports ir/dangling-prop-ref with
// it.
func TestRefNoun_NamesTheReferenceClass(t *testing.T) {
	t.Parallel()
	for want, idType := range map[string]reflect.Type{
		"type":    reflect.TypeFor[ir.TypeID](),
		"channel": reflect.TypeFor[ir.ChannelID](),
		"message": reflect.TypeFor[ir.MessageID](),
		"auth":    reflect.TypeFor[ir.AuthID](),
		"prop":    reflect.TypeFor[ir.PropID](),
	} {
		assert.Equal(t, want, ir.RefNoun(idType))
	}
}
