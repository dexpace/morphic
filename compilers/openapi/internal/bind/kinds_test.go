// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// The kinds the rows below spell, by their initials.
const (
	kRaw = bind.KindRaw
	kOth = bind.KindOther
	kCom = bind.KindComponents
	kMap = bind.KindSchemaMap
	kSch = bind.KindSchema
	kOp  = bind.KindOperation
	kPar = bind.KindParameter
	kHdr = bind.KindHeader
	kBod = bind.KindRequestBody
	kRes = bind.KindResponse
	kMT  = bind.KindMediaType
)

// TestKinds_ReadsEachPositionByTheGrammar pins the kind the source's own
// grammar reads at each position on the way to a pointer, the root's first,
// whether or not the document writes anything there.
func TestKinds_ReadsEachPositionByTheGrammar(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components: {schemas: {A: {type: object}}}\n", self)
	tests := []struct {
		pointer jsontext.Pointer
		want    []bind.Kind
	}{
		{"", []bind.Kind{kOth}},
		{"/components/schemas/A/properties/x", []bind.Kind{kOth, kCom, kMap, kSch, kMap, kSch}},
		{"/components/schemas/A/oneOf/0/items", []bind.Kind{kOth, kCom, kMap, kSch, kOth, kSch, kSch}},
		{"/components/schemas/A/enum/0", []bind.Kind{kOth, kCom, kMap, kSch, kOth, kRaw}},
		{"/paths/~1a/post/requestBody/content/application~1json/schema",
			[]bind.Kind{kOth, kOth, kOth, kOp, kBod, kOth, kMT, kSch}},
		{"/paths/~1a/get/parameters/0/schema", []bind.Kind{kOth, kOth, kOth, kOp, kOth, kPar, kSch}},
		{"/paths/~1a/get/responses/200/headers/X/schema", []bind.Kind{kOth, kOth, kOth, kOp, kOth, kRes, kOth, kHdr, kSch}},
		{"/info/title", []bind.Kind{kOth, kOth, kRaw}},
		{"/x-ext/a", []bind.Kind{kOth, kRaw, kRaw}},
	}
	for _, tc := range tests {
		t.Run(string(tc.pointer), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, table.Kinds(at(tc.pointer)))
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
	// A reference object is read where the parser resolved it, as the load
	// phase hands the binder.
	cfg.Through = func(name string, p jsontext.Pointer) (bind.ReferenceEnd, bool) {
		if name != "/spec.yaml" || p != "/components/parameters/P" {
			return bind.ReferenceEnd{}, false
		}
		return bind.ReferenceEnd{Name: "/params.yaml", Pointer: "/Size"}, true
	}
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

	assert.Equal(t, []bind.Kind{kOth, kRaw, kSch, kMap, kSch}, table.Kinds(at("/x-lib/Thing/properties/p")))
	assert.Equal(t, []bind.Kind{kOth, kRaw, kRaw}, table.Kinds(at("/x-lib/Other")), "no reference reads it")
	assert.Equal(t, []bind.Kind{kRaw, kPar, kSch}, table.Kinds(bind.Position{Doc: params, Pointer: "/Size/schema"}))
	assert.Equal(t, []bind.Kind{kSch, kMap, kSch}, table.Kinds(bind.Position{Doc: pet, Pointer: "/properties/p"}))
}

// TestKinds_KeepsWhatTheSourceWrites pins that a position the source's own
// grammar reads is that, whatever a reference landing on it expects: a
// parameter's $ref to a schema, which the binder reads as a parameter, does
// not make the schema a parameter.
func TestKinds_KeepsWhatTheSourceWrites(t *testing.T) {
	t.Parallel()
	cfg := self
	cfg.Through = func(name string, p jsontext.Pointer) (bind.ReferenceEnd, bool) {
		if p != "/components/parameters/P" {
			return bind.ReferenceEnd{}, false
		}
		return bind.ReferenceEnd{Name: name, Pointer: "/components/schemas/S"}, true
	}
	table := bound(t, head+`components:
  schemas:
    S: {name: s, in: query}
  parameters:
    P: {$ref: '#/components/schemas/S'}
`, cfg)
	assert.Equal(t, []bind.Kind{kOth, kCom, kMap, kSch}, table.Kinds(at("/components/schemas/S")))
}

// TestKinds_ReadsTheLesserOfTwoReadings pins a raw position two references
// read as two kinds: the lesser counts, so the answer depends neither on which
// reference the document declares first nor on which the binder reaches
// first. In the last row the schema reading arrives a round after the
// parameter's, through a property of U, which only a walk of U finds.
func TestKinds_ReadsTheLesserOfTwoReadings(t *testing.T) {
	t.Parallel()
	cfg := self
	cfg.Through = func(name string, p jsontext.Pointer) (bind.ReferenceEnd, bool) {
		if p != "/components/parameters/P" {
			return bind.ReferenceEnd{}, false
		}
		return bind.ReferenceEnd{Name: name, Pointer: "/x-lib/T"}, true
	}
	lib := "x-lib: {T: {name: t, in: query}, U: {properties: {t: {$ref: '#/x-lib/T'}}}}\ncomponents:\n"
	schema := "  schemas:\n    A: {$ref: '#/x-lib/T'}\n"
	later := "  schemas:\n    A: {$ref: '#/x-lib/U'}\n"
	parameter := "  parameters:\n    P: {$ref: '#/x-lib/T'}\n"
	for name, src := range map[string]string{
		"the schema first":                 lib + schema + parameter,
		"the parameter first":              lib + parameter + schema,
		"the schema reached a round later": lib + later + parameter,
	} {
		assert.Equal(t, []bind.Kind{kOth, kRaw, kSch}, bound(t, head+src, cfg).Kinds(at("/x-lib/T")), name)
	}
}

// TestKinds_StopsAtTheTokenCap pins that Kinds reads a pointer through as
// many tokens as the table reads any pointer through, and no further.
func TestKinds_StopsAtTheTokenCap(t *testing.T) {
	t.Parallel()
	table := bound(t, head, self)
	within := table.Kinds(at(jsontext.Pointer(strings.Repeat("/x-p", bind.MaxPointerTokens))))
	past := table.Kinds(at(jsontext.Pointer(strings.Repeat("/x-p", bind.MaxPointerTokens+1))))
	assert.Len(t, within, bind.MaxPointerTokens+1, "every token is read")
	assert.Len(t, past, bind.MaxPointerTokens+1, "the one past the cap is not")
}
