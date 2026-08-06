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

// declaringDoc declares one node of every class that carries its own ID, so a
// test can ask what the value graph yields for each.
func declaringDoc() *ir.Document {
	return &ir.Document{
		Types: ir.TypeRegistry{
			"t/x/M": &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M"}, Properties: []ir.Property{
				{ID: "p/x/M/f", Type: ir.TypeRef{Target: "t/x/M"}},
			}},
		},
		Channels: map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x"}},
		Messages: map[ir.MessageID]ir.Message{"m/x": {ID: "m/x"}},
		Auth:     map[ir.AuthID]ir.AuthScheme{"auth/x": {ID: "auth/x", Kind: ir.AuthKindAPIKey}},
		Services: []ir.Service{{
			ID:     "s/x",
			Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "op/x"}}}},
		}},
	}
}

// declaredBy indexes declarations by ID class name, so a case can assert on one
// class without depending on walk order.
func declaredBy(decls []ir.IDDeclaration) map[string][]ir.IDDeclaration {
	out := map[string][]ir.IDDeclaration{}
	for _, d := range decls {
		out[d.Class.Name()] = append(out[d.Class.Name()], d)
	}
	return out
}

// TestDeclaredIDs_ReachesEveryIDBearingNodeOnce pins the derivation the OpID and
// ServiceID registries are built from: every node carrying an ID of its own
// contributes exactly one declaration, at the path it sits at.
//
// The "once" half is the load-bearing one. Every type node embeds TypeCommon and
// promotes its ID, so a walk counting promoted fields would report a sound
// document as one where every type ID is declared twice.
func TestDeclaredIDs_ReachesEveryIDBearingNodeOnce(t *testing.T) {
	t.Parallel()
	decls, truncated := ir.DeclaredIDs(declaringDoc())
	require.False(t, truncated, "a shallow document must not exhaust the walk")

	byClass := declaredBy(decls)
	want := map[string]ir.IDDeclaration{
		"TypeID":    {ID: "t/x/M", Path: "doc.Types[t/x/M]"},
		"PropID":    {ID: "p/x/M/f", Path: "doc.Types[t/x/M].Properties[0]"},
		"ChannelID": {ID: "c/x", Path: "doc.Channels[c/x]"},
		"MessageID": {ID: "m/x", Path: "doc.Messages[m/x]"},
		"AuthID":    {ID: "auth/x", Path: "doc.Auth[auth/x]"},
		"ServiceID": {ID: "s/x", Path: "doc.Services[0]"},
		"OpID":      {ID: "op/x", Path: "doc.Services[0].Groups[0].Operations[0]"},
	}
	for class, w := range want {
		got := byClass[class]
		require.Len(t, got, 1, "%s is declared by exactly one node", class)
		assert.Equal(t, w.ID, got[0].ID, class)
		assert.Equal(t, w.Path, got[0].Path, class)
	}
	assert.Len(t, byClass, len(want), "these are every ID class a node declares")
}

// TestDeclaredIDs_SkipsEmptyAndAbsentIDs holds the two ways a struct declares no
// identity: it carries no ID field at all, or it carries an empty one. An empty
// ID names nothing a reference could resolve to, and treating several of them as
// one another's duplicates would name the wrong defect.
func TestDeclaredIDs_SkipsEmptyAndAbsentIDs(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Name:     "no ID field anywhere on Document itself",
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{}}}}}},
	}
	decls, truncated := ir.DeclaredIDs(doc)
	assert.False(t, truncated)
	assert.Empty(t, decls)
}

// TestDeclaredIDs_NilDocumentDeclaresNothing keeps the derivation report-only:
// a nil document yields no declaration rather than panicking on the way to
// saying so, as DocumentRegistries does.
func TestDeclaredIDs_NilDocumentDeclaresNothing(t *testing.T) {
	t.Parallel()
	decls, truncated := ir.DeclaredIDs(nil)
	assert.Empty(t, decls)
	assert.False(t, truncated)
}

// TestWithDeclarations_ResolvesOnlyTheClassesNoMapCovers pins which classes the
// declaration-derived registries answer for. A class Document keys a map by keeps
// that map, PropID stays out because a property is a position inside its model,
// and what is left — OpID and ServiceID — is exactly what resolved against
// nothing before.
func TestWithDeclarations_ResolvesOnlyTheClassesNoMapCovers(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	decls, _ := ir.DeclaredIDs(doc)
	regs := ir.DocumentRegistries(doc).WithDeclarations(decls)

	for _, tc := range []struct {
		class    reflect.Type
		declared string
		label    string
	}{
		{reflect.TypeFor[ir.OpID](), "op/x", "op declarations"},
		{reflect.TypeFor[ir.ServiceID](), "s/x", "service declarations"},
	} {
		reg, resolved := regs[tc.class]
		require.True(t, resolved, "%s must resolve against the nodes declaring it", tc.class.Name())
		assert.Equal(t, tc.label, reg.Label)
		assert.True(t, reg.Has(tc.declared))
		assert.False(t, reg.Has(tc.declared+"/ghost"))
	}

	_, propsResolved := regs[reflect.TypeFor[ir.PropID]()]
	assert.False(t, propsResolved, "a PropID is a position inside its model, not a document-level identity")
	assert.Equal(t, "types", regs[reflect.TypeFor[ir.TypeID]()].Label,
		"a class Document keys a map by keeps that map as its registry")
}

// TestWithDeclarations_LeavesTheReceiverAlone holds the copy at the boundary: the
// two checkers each derive their own view, and a call that grew the map it was
// handed would leak one caller's classes into another's.
func TestWithDeclarations_LeavesTheReceiverAlone(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	decls, _ := ir.DeclaredIDs(doc)
	regs := ir.DocumentRegistries(doc)
	before := len(regs)

	extended := regs.WithDeclarations(decls)
	assert.Len(t, regs, before)
	assert.Greater(t, len(extended), before, "the declaration-derived classes must be added somewhere")
}

// TestWithDeclarations_SecondDeclarationJoinsTheSameRegistry drives the branch
// that reuses a registry already built for a class, so two operations both
// resolve rather than the second replacing the first.
func TestWithDeclarations_SecondDeclarationJoinsTheSameRegistry(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations,
		ir.Operation{ID: "op/y"})

	decls, _ := ir.DeclaredIDs(doc)
	ops := ir.DocumentRegistries(doc).WithDeclarations(decls)[reflect.TypeFor[ir.OpID]()]
	assert.True(t, ops.Has("op/x"))
	assert.True(t, ops.Has("op/y"))
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
