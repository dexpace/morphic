package jsonshape

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// TestDescribe_HoldsItsBounds generates a module whose roots reach exactly as
// many types as one description holds, and nest exactly as deep as one type
// may, and roots one past each. The first two describe and the last two are
// refused, so each bound is the limit it names rather than one off it.
//
// It changes directory, which a parallel test cannot.
func TestDescribe_HoldsItsBounds(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package bounds\n")
	chain := func(prefix string, n int) {
		for i := range n - 1 {
			fmt.Fprintf(&src, "type %s%d struct { N *%s%d `json:\"n\"` }\n", prefix, i, prefix, i+1)
		}
		fmt.Fprintf(&src, "type %s%d struct{}\n", prefix, n-1)
	}
	chain("Within", maxTypes)
	chain("Beyond", maxTypes+1)
	fmt.Fprintf(&src, "type Shallow struct { D %sstring `json:\"d\"` }\n", strings.Repeat("[]", maxDepth))
	fmt.Fprintf(&src, "type Deep struct { D %sstring `json:\"d\"` }\n", strings.Repeat("[]", maxDepth+1))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bounds\n\ngo 1.27\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bounds.go"), []byte(src.String()), 0o644))

	t.Chdir(dir)
	t.Setenv("GOWORK", "off")
	p, err := decls.Load(".")
	require.NoError(t, err)

	for _, root := range []string{"Within0", "Shallow"} {
		_, err := Describe(p, root)
		assert.NoError(t, err, root)
	}
	_, err = Describe(p, "Beyond0")
	assert.ErrorContains(t, err, fmt.Sprintf("Beyond0 reaches more than %d types", maxTypes))
	_, err = Describe(p, "Deep")
	assert.ErrorContains(t, err, fmt.Sprintf("Deep: field D: a type nests more than %d deep", maxDepth))
}
