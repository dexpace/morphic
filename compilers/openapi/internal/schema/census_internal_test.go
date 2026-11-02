// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestPreserveUnhomedKeywords_MissingNode drives the invariant no source can
// break: an ID a lowering returned that the registry does not hold. The keywords
// it would keep have nowhere to go, so the guard reports the broken invariant
// rather than preserving them onto nothing.
func TestPreserveUnhomedKeywords_MissingNode(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	got, diags := preserveUnhomedKeywords(l.Ctx, l.Types, &oas3.Schema{}, "/p", "t/anon/missing", dispatch{})
	assert.Equal(t, ir.TypeID("t/anon/missing"), got, "the lowering's own ID still stands")
	assertInternalInvariant(t, diags)
}

// TestRecordUnhomedKeywords_MissingOwner drives the same invariant one step
// later, where the owning node is the one absent. Its caller cannot produce this
// state — the owner is either the node it just looked up or one internAlias just
// interned — so it is reached directly, as preserveUnionSiblings' guard is.
func TestRecordUnhomedKeywords_MissingOwner(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	diags := recordUnhomedKeywords(l.Ctx, l.Types, "t/anon/missing", &oas3.Schema{}, []string{"items"}, nodeShape(ir.KindPrimitive), "/p")
	assertInternalInvariant(t, diags)
}

// TestPreserveVariantsUnhomed_NeedsAUnion drives the two states its callers
// cannot produce, each having just interned the Union under id: no node there,
// and a node of another kind, which has no variants to ask.
func TestPreserveVariantsUnhomed_NeedsAUnion(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	assertInternalInvariant(t, preserveVariantsUnhomed(l.Ctx, l.Types, &oas3.Schema{}, "t/anon/missing", distributedShape, nil, "/p"))

	l.Types.Register("t/anon/scalar", &ir.Scalar{ID: "t/anon/scalar"})
	assertInternalInvariant(t, preserveVariantsUnhomed(l.Ctx, l.Types, &oas3.Schema{}, "t/anon/scalar", distributedShape, nil, "/p"))
}

// TestKeywordHome_AnUnknownKeywordIsHomedNowhere reaches the arm censusKeywords
// cannot reach today, the way TestDeclaresFamily_AnUnknownNameDeclaresNothing
// reaches its own: every keyword unhomedKeywords asks about is one with a case
// here.
//
// "No home" is the safe half of that default. A keyword added to censusKeywords
// without an arm is kept verbatim beside every node it is written on — noisy,
// never lossy — where the opposite default would drop it in exactly the silence
// GitHub #268 was.
func TestKeywordHome_AnUnknownKeywordIsHomedNowhere(t *testing.T) {
	t.Parallel()
	assert.False(t, keywordHome(&ir.Model{}, &oas3.Schema{}, "unevaluatedItems"),
		"a keyword with no arm has no home on any node")
}

// TestKeywordHome_EveryCensusKeywordHasANodeThatCarriesIt pins the agreement
// censusKeywords and keywordHome's switch have to keep, in the direction the
// default cannot fake.
//
// A listed keyword with no arm answers "homeless" everywhere, and that is the
// answer a $ref site expects from every one of them, so no ref-site test can see
// the omission. Asking instead for the node kind that *does* carry the keyword
// is what makes it visible.
func TestKeywordHome_EveryCensusKeywordHasANodeThatCarriesIt(t *testing.T) {
	t.Parallel()
	// A declared scalar type, which is what homes `format` on a primitive and
	// `type` on a node of a scalar shape.
	s := &oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeString)}
	nodes := []ir.TypeDef{
		&ir.Model{}, &ir.List{}, &ir.Tuple{}, &ir.Literal{}, &ir.Enum{},
		&ir.Union{Discriminator: &ir.Discriminator{}},
		&ir.Scalar{Encoding: &ir.Encoding{}}, &ir.Primitive{},
	}
	require.NotEmpty(t, censusKeywords, "an empty census would make this pass vacuously")
	for _, keyword := range censusKeywords {
		homed := false
		for _, td := range nodes {
			homed = homed || keywordHome(td, s, keyword)
		}
		assert.True(t, homed, "%q is in the census with no node kind that carries it", keyword)
	}
}

// TestDiscriminatorHome_AsksWhetherTheFieldWasFilled pins what homes a
// discriminator: a Model or Union whose lowering filled the field. Having the
// field is not enough, since the Union a mixed-type enum lowers to holds none,
// and no other kind, nor the nil td of a position lowered to a reference, has
// the field at all.
func TestDiscriminatorHome_AsksWhetherTheFieldWasFilled(t *testing.T) {
	t.Parallel()
	filled := &ir.Discriminator{PropertyName: "kind"}
	cases := []struct {
		name string
		td   ir.TypeDef
		want bool
	}{
		{"model holding one", &ir.Model{Discriminator: filled}, true},
		{"union holding one", &ir.Union{Discriminator: filled}, true},
		{"model holding none", &ir.Model{}, false},
		{"union holding none", &ir.Union{}, false},
		{"scalar", &ir.Scalar{}, false},
		{"enum", &ir.Enum{}, false},
		{"reference", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, keywordHome(c.td, &oas3.Schema{}, "discriminator"))
		})
	}
}
