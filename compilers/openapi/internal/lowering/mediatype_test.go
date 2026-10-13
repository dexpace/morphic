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
