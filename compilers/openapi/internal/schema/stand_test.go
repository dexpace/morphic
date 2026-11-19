// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestStand_AUnionKeptBesideAPrincipalsBodyStandsAtThePosition pins an inline
// principal writing a union whose branches declare no shape beside a body that
// lowers to a shared primitive: the alias keeping the union stands at the
// position, as it does where the same keywords are written beside each other,
// the union kept where it is written (docs/compilers/openapi.md#allof-refinement).
func TestStand_AUnionKeptBesideAPrincipalsBodyStandsAtThePosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    Beside: {description: d, type: string, oneOf: [{minLength: 1}, {maxLength: 3}]}\n"+
			"    Entry: {allOf: [{description: d}, {type: string, oneOf: [{minLength: 1}, {maxLength: 3}]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for name, at := range map[string]string{"Beside": "/components/schemas/Beside/oneOf", "Entry": "/components/schemas/Entry/allOf/1/oneOf"} {
		alias, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is the alias keeping the union, not %T", name, typeByName(doc, name))
		require.NotNil(t, alias.Base)
		assert.Equal(t, ir.TypeID("type/primitive/string"), alias.Base.Target, name)
		assert.Equal(t, "d", alias.Docs.Description, name)
		kept, held := alias.Unmodeled["openapi:oneOf"]
		require.True(t, held, name)
		assert.Equal(t, ir.ReasonValidationOnly, kept.Reason, name)
		assert.Equal(t, at, string(kept.Provenance.Pointer), "%s: kept where it is written", name)
	}
	_, atEntry := doc.Types["type/anonymous/components/schemas/Entry/allOf/1"]
	assert.False(t, atEntry, "nothing stands at the entry")
}

// TestStand_ContentVocabularyIsReadWhereThePrincipalsNodeHoldsIt pins the
// content vocabulary an inline principal writes beside its type: the Scalar a
// string builds holds it in its Encoding, keeping nothing, and the Enum a value
// set builds, which has no Encoding, keeps it, each as the same keywords beside
// each other do.
func TestStand_ContentVocabularyIsReadWhereThePrincipalsNodeHoldsIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    ScalarBeside: {description: d, type: string, contentEncoding: base64}\n"+
			"    ScalarEntry: {allOf: [{description: d}, {type: string, contentEncoding: base64}]}\n"+
			"    SetBeside: {description: d, type: string, contentEncoding: base64, enum: [a]}\n"+
			"    SetEntry: {allOf: [{description: d}, {type: string, contentEncoding: base64, enum: [a]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"ScalarBeside", "ScalarEntry"} {
		sc, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is the scalar, not %T", name, typeByName(doc, name))
		require.NotNil(t, sc.Encoding, name)
		assert.Equal(t, "base64", sc.Encoding.Name, name)
		assert.Empty(t, sc.Unmodeled, "%s keeps nothing", name)
	}
	for _, name := range []string{"SetBeside", "SetEntry"} {
		enum, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is the enum, not %T", name, typeByName(doc, name))
		kept, held := enum.Unmodeled["openapi:contentEncoding"]
		require.True(t, held, "%s keeps what no Encoding holds", name)
		assert.Equal(t, ir.ReasonNoIRHome, kept.Reason, name)
	}
}

// TestStand_ContentVocabularyItsNodeCannotHoldIsKept pins the content
// vocabulary an inline principal writes beside a union, a null alone or an
// inferred list, whose node has no Encoding: it is kept on that node where it
// is written, as the same keywords beside each other keep it
// (docs/compilers/openapi.md#content-vocabulary).
func TestStand_ContentVocabularyItsNodeCannotHoldIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    UnionBeside: {description: d, oneOf: [{type: string}, {type: integer}], contentMediaType: text/plain}\n"+
			"    UnionEntry: {allOf: [{description: d}, {oneOf: [{type: string}, {type: integer}], contentMediaType: text/plain}]}\n"+
			"    NullEntry: {allOf: [{description: d}, {oneOf: [{type: 'null'}], contentMediaType: text/plain}]}\n"+
			"    ListEntry: {allOf: [{description: d}, {items: {type: string}, contentMediaType: text/plain}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for name, at := range map[string]string{
		"UnionBeside": "/components/schemas/UnionBeside/contentMediaType",
		"UnionEntry":  "/components/schemas/UnionEntry/allOf/1/contentMediaType",
		"NullEntry":   "/components/schemas/NullEntry/allOf/1/contentMediaType",
		"ListEntry":   "/components/schemas/ListEntry/allOf/1/contentMediaType",
	} {
		td := typeByName(doc, name)
		require.NotNil(t, td, name)
		kept, held := td.Common().Unmodeled["openapi:contentMediaType"]
		require.True(t, held, "%s keeps what no Encoding holds, on %T", name, td)
		assert.Equal(t, ir.ReasonNoIRHome, kept.Reason, name)
		assert.Equal(t, at, string(kept.Provenance.Pointer), "%s: kept where it is written", name)
	}
}

// TestStand_ContentBesideTheAllOfIsKeptBesideThePrincipalsEncoding pins a
// content keyword the position writes beside an allOf whose inline principal
// builds a Scalar holding an Encoding of its own: the Encoding is the branch's,
// so the keyword is kept beside it where it is written, never dropped, where
// the keywords beside an allOf electing no entry build the Encoding holding it
// (docs/compilers/openapi.md#content-vocabulary).
func TestStand_ContentBesideTheAllOfIsKeptBesideThePrincipalsEncoding(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    Beside: {allOf: [{type: string, contentEncoding: base64}], contentMediaType: text/plain}\n"+
			"    Own: {type: string, contentEncoding: base64, allOf: [{minLength: 1}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	own, ok := typeByName(doc, "Own").(*ir.Scalar)
	require.True(t, ok, "Own is the scalar, not %T", typeByName(doc, "Own"))
	require.NotNil(t, own.Encoding)
	assert.Equal(t, "base64", own.Encoding.Name)
	assert.NotContains(t, own.Unmodeled, "openapi:contentEncoding", "Own's Encoding holds what it writes")
	sc, ok := typeByName(doc, "Beside").(*ir.Scalar)
	require.True(t, ok, "Beside is the scalar, not %T", typeByName(doc, "Beside"))
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "base64", sc.Encoding.Name, "the branch's Encoding stands")
	assert.Empty(t, sc.Encoding.MediaType, "and holds nothing the position writes")
	kept, held := sc.Unmodeled["openapi:contentMediaType"]
	require.True(t, held, "the position's keyword is kept")
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.Equal(t, "/components/schemas/Beside/contentMediaType", string(kept.Provenance.Pointer))
}
