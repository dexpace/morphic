// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// The kinds the rows below spell, by their initials.
const (
	kRaw = reftable.KindRaw
	kOth = reftable.KindOther
	kCom = reftable.KindComponents
	kMap = reftable.KindSchemaMap
	kSch = reftable.KindSchema
	kOp  = reftable.KindOperation
	kPar = reftable.KindParameter
	kHdr = reftable.KindHeader
	kBod = reftable.KindRequestBody
	kRes = reftable.KindResponse
	kMT  = reftable.KindMediaType
	kEnc = reftable.KindEncoding
)

// TestKinds_ReadsEachPositionByTheGrammar pins the kind the source's own
// grammar reads at each position on the way to a pointer, the root's first,
// whether or not the document writes anything there.
func TestKinds_ReadsEachPositionByTheGrammar(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components: {schemas: {A: {type: object}}}\n", self)
	tests := []struct {
		pointer jsontext.Pointer
		want    []reftable.Kind
	}{
		{"", []reftable.Kind{kOth}},
		{"/components/schemas/A/properties/x", []reftable.Kind{kOth, kCom, kMap, kSch, kMap, kSch}},
		{"/components/schemas/A/oneOf/0/items", []reftable.Kind{kOth, kCom, kMap, kSch, kOth, kSch, kSch}},
		{"/components/schemas/A/enum/0", []reftable.Kind{kOth, kCom, kMap, kSch, kOth, kRaw}},
		{"/paths/~1a/post/requestBody/content/application~1json/schema",
			[]reftable.Kind{kOth, kOth, kOth, kOp, kBod, kOth, kMT, kSch}},
		{"/paths/~1a/get/parameters/0/schema", []reftable.Kind{kOth, kOth, kOth, kOp, kOth, kPar, kSch}},
		{"/paths/~1a/get/responses/200/headers/X/schema", []reftable.Kind{kOth, kOth, kOth, kOp, kOth, kRes, kOth, kHdr, kSch}},
		{"/paths/~1a/post/requestBody/content/multipart~1form-data/encoding/f/headers/X/schema",
			[]reftable.Kind{kOth, kOth, kOth, kOp, kBod, kOth, kMT, kOth, kEnc, kOth, kHdr, kSch}},
		{"/paths/~1a/post/requestBody/content/multipart~1mixed/itemEncoding/encoding/g/headers/X/schema",
			[]reftable.Kind{kOth, kOth, kOth, kOp, kBod, kOth, kMT, kEnc, kOth, kEnc, kOth, kHdr, kSch}},
		{"/info/title", []reftable.Kind{kOth, kOth, kRaw}},
		{"/x-ext/a", []reftable.Kind{kOth, kRaw, kRaw}},
	}
	for _, tc := range tests {
		t.Run(string(tc.pointer), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, table.Kinds(documentPointerAt(tc.pointer)))
		})
	}
}

// TestKinds_ReadsWhatAReferenceReads pins a position the grammar reads as raw
// YAML, or one in another document: it is read as the reference that lands on
// it reads it, a schema an extension holds or a parameter another document
// holds, and another document's root as a schema where a schema's $ref names
// the whole document. A position no reference reads stays raw.
func TestKinds_ReadsWhatAReferenceReads(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{
		"/params.yaml": "Size: {name: size, in: query, schema: {type: object}}\n",
		"/pet.yaml":    "type: object\n",
	})
	cfg.Self, cfg.Locate = resolving.Self, resolving.Locate
	table := bound(t, head+`x-lib: {Thing: {type: object}, Other: {type: object}}
components:
  schemas:
    A: {$ref: '#/x-lib/Thing'}
    B: {$ref: 'pet.yaml'}
  parameters:
    P: {$ref: 'params.yaml#/Size'}
`, cfg)
	params, ok := table.Find("/params.yaml")
	require.True(t, ok)
	pet, ok := table.Find("/pet.yaml")
	require.True(t, ok)

	assert.Equal(t, []reftable.Kind{kOth, kRaw, kSch, kMap, kSch}, table.Kinds(documentPointerAt("/x-lib/Thing/properties/p")))
	assert.Equal(t, []reftable.Kind{kOth, kRaw, kRaw}, table.Kinds(documentPointerAt("/x-lib/Other")), "no reference reads it")
	assert.Equal(t, []reftable.Kind{kRaw, kPar, kSch}, table.Kinds(reftable.DocumentPointer{Doc: params, Pointer: "/Size/schema"}))
	assert.Equal(t, []reftable.Kind{kSch, kMap, kSch}, table.Kinds(reftable.DocumentPointer{Doc: pet, Pointer: "/properties/p"}))
}

// TestKinds_KeepsWhatTheSourceWrites pins that a position the source's own
// grammar reads is that, whatever a reference landing on it expects: a
// parameter's $ref to a schema, which names no parameter, does not make the
// schema one.
func TestKinds_KeepsWhatTheSourceWrites(t *testing.T) {
	t.Parallel()
	cfg := self
	table := bound(t, head+`components:
  schemas:
    S: {name: s, in: query}
  parameters:
    P: {$ref: '#/components/schemas/S'}
`, cfg)
	assert.Equal(t, []reftable.Kind{kOth, kCom, kMap, kSch}, table.Kinds(documentPointerAt("/components/schemas/S")))
}

// TestKinds_ReadsTheLesserOfTwoReadings pins a raw position two references
// read as two kinds: the lesser counts, so the answer depends neither on which
// reference the document declares first nor on which the binder reaches
// first. In the last row the schema reading arrives a round after the
// parameter's, through a property of U, which only a walk of U finds.
func TestKinds_ReadsTheLesserOfTwoReadings(t *testing.T) {
	t.Parallel()
	cfg := self
	lib := "x-lib: {T: {name: t, in: query}, U: {properties: {t: {$ref: '#/x-lib/T'}}}}\ncomponents:\n"
	schema := "  schemas:\n    A: {$ref: '#/x-lib/T'}\n"
	later := "  schemas:\n    A: {$ref: '#/x-lib/U'}\n"
	parameter := "  parameters:\n    P: {$ref: '#/x-lib/T'}\n"
	for name, src := range map[string]string{
		"the schema first":                 lib + schema + parameter,
		"the parameter first":              lib + parameter + schema,
		"the schema reached a round later": lib + later + parameter,
	} {
		assert.Equal(t, []reftable.Kind{kOth, kRaw, kSch}, bound(t, head+src, cfg).Kinds(documentPointerAt("/x-lib/T")), name)
	}
}

// TestKinds_StopsAtTheTokenCap pins that Kinds reads a pointer through as
// many tokens as the table reads any pointer through, and no further.
func TestKinds_StopsAtTheTokenCap(t *testing.T) {
	t.Parallel()
	table := bound(t, head, self)
	within := table.Kinds(documentPointerAt(jsontext.Pointer(strings.Repeat("/x-p", reftable.MaxPointerTokens))))
	past := table.Kinds(documentPointerAt(jsontext.Pointer(strings.Repeat("/x-p", reftable.MaxPointerTokens+1))))
	assert.Len(t, within, reftable.MaxPointerTokens+1, "every token is read")
	assert.Len(t, past, reftable.MaxPointerTokens+1, "the one past the cap is not")
}

// TestReadingAt_ReadsWhatKindsReadsLast pins a Reading against Kinds: the
// position Kinds ends at is a schema exactly where ReadingAt reads one, and
// can stand for a reference object exactly where ReadingAt reads that, in the
// source and in a document only a reference reads.
func TestReadingAt_ReadsWhatKindsReadsLast(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{"/params.yaml": "Size: {name: size, in: query}\n"})
	cfg.Self, cfg.Locate = resolving.Self, resolving.Locate
	table := bound(t, head+"components: {parameters: {P: {$ref: 'params.yaml#/Size'}}}\n", cfg)
	params, ok := table.Find("/params.yaml")
	require.True(t, ok)
	tests := []struct {
		pos               reftable.DocumentPointer
		schema, referable bool
	}{
		{documentPointerAt(""), false, false},
		{documentPointerAt("/components/parameters/P"), false, true},
		{documentPointerAt("/components/parameters/P/schema"), true, false},
		{documentPointerAt("/paths/~1a/get/responses/200/headers/X"), false, true},
		{documentPointerAt("/paths/~1a/get/callbacks/c/{$url}"), false, true},
		{documentPointerAt("/components/securitySchemes/S"), false, true},
		{documentPointerAt("/components/examples/E/value"), false, false},
		{documentPointerAt("/x-ext/schema"), false, false},
		{reftable.DocumentPointer{Doc: params, Pointer: "/Size"}, false, true},
		{reftable.DocumentPointer{Doc: params, Pointer: "/Size/schema"}, true, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.pos.Pointer), func(t *testing.T) {
			t.Parallel()
			r := table.ReadingAt(tc.pos)
			kinds := table.Kinds(tc.pos)
			assert.Equal(t, kinds[len(kinds)-1] == reftable.KindSchema, r.Schema(), "as Kinds reads it")
			assert.Equal(t, tc.schema, r.Schema())
			assert.Equal(t, tc.referable, r.Referable())
		})
	}
}

// TestReading_BelowStepsTheGrammar pins Below: from a parameter, its schema
// is a schema, and so is a media type's beneath its content, while beneath an
// example's value or an extension a schema key is raw YAML, as is a header's
// schema beneath an Encoding Object another nests, though not beneath the one
// nesting it.
func TestReading_BelowStepsTheGrammar(t *testing.T) {
	t.Parallel()
	param := bound(t, head, self).ReadingAt(documentPointerAt("/components/parameters/P"))
	require.True(t, param.Referable())
	assert.True(t, param.Below("schema").Schema())
	assert.True(t, param.Below("content").Below("application/json").Below("schema").Schema())
	encoding := param.Below("content").Below("multipart/form-data").Below("encoding").Below("f")
	headerSchema := func(r reftable.Reading) bool { return r.Below("headers").Below("H").Below("schema").Schema() }
	assert.True(t, headerSchema(encoding))
	assert.False(t, headerSchema(encoding.Below("encoding").Below("g")))
	assert.False(t, headerSchema(encoding.Below("prefixEncoding").Below("0")))
	assert.False(t, headerSchema(encoding.Below("itemEncoding")))
	assert.False(t, param.Below("examples").Below("e").Below("value").Below("schema").Schema())
	assert.False(t, param.Below("x-shape").Below("schema").Schema())
	assert.True(t, param.Below("examples").Below("e").Referable(), "an example can be a $ref")
}

// TestReadingAt_ReadsRawWhereItPlacesNothing pins the zero Reading, which
// reads raw YAML: what a nil table gives, and a pointer past the tokens Kinds
// reads, where the position it ends at, a schema here, is not the one asked
// for.
func TestReadingAt_ReadsRawWhereItPlacesNothing(t *testing.T) {
	t.Parallel()
	var none *reftable.ReferenceTargetTable
	assert.Equal(t, reftable.Reading{}, none.ReadingAt(documentPointerAt("/components/parameters/P")))
	assert.False(t, reftable.Reading{}.Below("schema").Schema())
	assert.False(t, reftable.Reading{}.Referable())

	table := bound(t, head, self)
	within := jsontext.Pointer("/components/schemas/A" + strings.Repeat("/not", reftable.MaxPointerTokens-3))
	require.True(t, table.ReadingAt(documentPointerAt(within)).Schema(), "every token is read")
	assert.Equal(t, reftable.Reading{}, table.ReadingAt(documentPointerAt(within+"/not")), "the one past the cap is not")
}
