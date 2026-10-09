package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestNamespaces_DeclarationIsCanonical pins the form ir.Document.IDSpaces
// requires: each list sorted and without repeats, whatever order and
// repetition the compiler named its constants in.
func TestNamespaces_DeclarationIsCanonical(t *testing.T) {
	t.Parallel()
	got := compile.Namespaces{
		ir.IDKindType:  {"openapi", "anon", "openapi", "composed"},
		ir.IDKindGroup: {"webhooks", "default"},
	}.Declaration()

	assert.Equal(t, map[string][]string{
		ir.IDKindType:  {"anon", "composed", "openapi"},
		ir.IDKindGroup: {"default", "webhooks"},
	}, got)
}

// TestNamespaces_DeclarationOmitsWhatIsEmpty pins that nothing declared is
// nothing written: a kind with no namespace has no key, and a compiler naming
// none yields nil, which the document omits.
func TestNamespaces_DeclarationOmitsWhatIsEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, compile.Namespaces(nil).Declaration())
	assert.Nil(t, compile.Namespaces{ir.IDKindOp: nil}.Declaration())
	assert.Equal(t, map[string][]string{ir.IDKindOp: {"openapi"}},
		compile.Namespaces{ir.IDKindOp: {"openapi"}, ir.IDKindProp: {}}.Declaration())
}

// TestNamespaces_DeclarationSharesNothingWithItsSource pins the copy: a caller
// that edits the document's declaration must not edit the compiler's.
func TestNamespaces_DeclarationSharesNothingWithItsSource(t *testing.T) {
	t.Parallel()
	source := compile.Namespaces{ir.IDKindOp: {"openapi"}}
	declaration := source.Declaration()
	require.Equal(t, []string{"openapi"}, declaration[ir.IDKindOp])

	declaration[ir.IDKindOp][0] = "edited"
	assert.Equal(t, []compile.Space{"openapi"}, source[ir.IDKindOp])
}
