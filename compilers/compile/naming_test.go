package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestNamingFor_KeepsTheSpellingAndTheWords pins the pairing that makes a Naming
// neutral: the source spelling is preserved verbatim for anyone who needs the
// original, and the words beside it carry no casing an emitter should own.
func TestNamingFor_KeepsTheSpellingAndTheWords(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Source: "com.example.User", Canonical: "com_example_user"},
		compile.NamingFor("com.example.User"))
	assert.Equal(t, ir.Naming{Source: "***", Canonical: ""}, compile.NamingFor("***"),
		"a spelling with no words still keeps its spelling")
}
