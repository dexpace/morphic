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

// TestMergeKept_AKeyHeldWithAnotherValueIsAnInvariantBroken pins what no source
// reaches: entryKeeps keeps an entry whole where another conjunct writes one of
// its keywords otherwise, so a key held with another value is reported broken
// and the entry held keeps its value, and a key nothing holds is written.
func TestMergeKept_AKeyHeldWithAnotherValueIsAnInvariantBroken(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	held := ir.Unmodeled{"openapi:format": {Value: ir.RawValue(`"a"`)}}
	p := ir.Unmodeled{"openapi:format": {Value: ir.RawValue(`"b"`)}}
	assertInternalInvariant(t, mergeKept(l.Ctx, &p, held, "/p/allOf/1"))
	assert.Equal(t, ir.RawValue(`"b"`), p["openapi:format"].Value, "what is held stands")

	var empty ir.Unmodeled
	require.Empty(t, mergeKept(l.Ctx, &empty, held, "/p/allOf/1"))
	assert.Equal(t, held, empty)
}

// TestKeepEntryDynamicRef_AnEntryWritingNoneKeepsNothing pins that a dynamic
// reference an entry does not write is kept and reported nowhere.
func TestKeepEntryDynamicRef_AnEntryWritingNoneKeepsNothing(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	var p ir.Unmodeled
	assert.Empty(t, keepEntryDynamicRef(l.Ctx, &p, StandaloneSchema(&oas3.Schema{}), "$dynamicRef", "/p/allOf/1"))
	assert.Empty(t, p)
}

// TestWritesTopLevelRef_TellsAKeptRefFromAnInlineEntry pins the reading that
// keeps dropFoldedIntoWhole from asking a kept $ref what it holds: a $ref at
// the top of an object only, and nothing of a value no object is or that does
// not decode.
func TestWritesTopLevelRef_TellsAKeptRefFromAnInlineEntry(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		`{"$ref":"#/a"}`:                      true,
		`{"x-a":1,"$ref":"#/a"}`:              true,
		`{"properties":{"p":{"$ref":"#/a"}}}`: false,
		`false`:                               false,
		`{"a"`:                                false,
		`{"a`:                                 false,
		`{`:                                   false,
		``:                                    false,
	} {
		assert.Equal(t, want, writesTopLevelRef(ir.RawValue(raw)), raw)
	}
}
