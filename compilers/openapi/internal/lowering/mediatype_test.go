// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestNormalizeMediaType_CollapsesTheSpellingsOneMediaTypeHas pins the rule
// every media-type classification in the compiler reads: the type and subtype
// are case-insensitive (RFC 6838 §4.2), parameters describe how a body carrying
// the type is encoded rather than naming another type, and surrounding space is
// not part of the name.
func TestNormalizeMediaType_CollapsesTheSpellingsOneMediaTypeHas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already normal", "application/json", "application/json"},
		{"case folded", "Application/JSON", "application/json"},
		{"parameters dropped", "application/json; charset=utf-8", "application/json"},
		{"space before the parameters trimmed", "application/json ; charset=utf-8", "application/json"},
		{"space around the whole type trimmed", "  application/json  ", "application/json"},
		{"all three at once", "  Application/Octet-Stream ; charset=utf-8  ", "application/octet-stream"},
		{"a parameter is not a type", ";charset=utf-8", ""},
		{"empty names nothing", "", ""},
		{"blank names nothing", "   ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lowering.NormalizeMediaType(tc.in))
		})
	}
}

// TestHoldsOneDocument_AnswersOnlyForJSONAndXML holds the predicate to the syntaxes
// known to hold one document. A json-seq suffix ends in neither suffix it reads,
// and anything unnamed answers false: a vendor type may well repeat a structure.
func TestHoldsOneDocument_AnswersOnlyForJSONAndXML(t *testing.T) {
	t.Parallel()
	for mediaType, want := range map[string]bool{
		"application/json":                true,
		"Application/JSON; charset=utf-8": true,
		"application/problem+json":        true,
		"application/xml":                 true,
		"text/xml":                        true,
		"application/atom+xml":            true,
		"application/json-seq":            false,
		"application/geo+json-seq":        false,
		"application/jsonl":               false,
		"application/x-ndjson":            false,
		"text/event-stream":               false,
		"multipart/mixed":                 false,
		"application/vnd.acme.frames":     false,
		"":                                false,
	} {
		assert.Equal(t, want, lowering.HoldsOneDocument(mediaType), "%q", mediaType)
	}
}
