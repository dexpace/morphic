package graphql_test

import (
	"net/url"
	"testing"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestFixtures breaks every rule, as a test building its fixtures does, which
// draws nothing in a test file.
func TestFixtures(t *testing.T) {
	reg := ir.TypeRegistry{}
	reg["t/graphql/fixture"] = nil
	n := ir.Naming{Canonical: "fixture"}
	n.Hint = "fixture"
	_, err := url.QueryUnescape("a+b")
	_ = compile.OpID(compile.Space("graphql"+"x"), "")
	if err != nil || n.Hint == "" {
		t.Fatal(err)
	}
}
