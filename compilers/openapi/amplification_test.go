package openapi

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

const amplificationBombFixture = "../../testdata/openapi/amplification_alias_bomb.yaml"

func TestCompile_AliasBombDoesNotExhaustMemory(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	type result struct {
		doc   *ir.Document
		diags []ir.Diagnostic
		err   error
	}
	done := make(chan result, 1)
	go func() {
		doc, diags, cerr := New().Compile(t.Context(),
			[]compilers.Source{{Path: "amplification_alias_bomb.yaml", Data: data}}, compilers.Options{})
		done <- result{doc, diags, cerr}
	}()

	const bound = 5 * time.Second
	select {
	case r := <-done:
		require.NoError(t, r.err, "an alias bomb is a spec problem, not a Go error")
		assert.Nil(t, r.doc, "the compiler refuses to lower an amplifying document")
		assertHasErrorCode(t, r.diags, diag.AliasAmplification)
	case <-time.After(bound):
		t.Fatalf("Compile did not return within %v — the bomb likely reached soa.Unmarshal", bound)
	}
}

// TestCompile_SharedCompositionAncestorsDoNotAmplify pins the visited set in the
// discriminator ancestor walk (compilers/openapi/internal/schema/compose.go) on
// an acyclic shape: a chain of diamonds, each allOf naming one parent twice.
// TestAllOf_DiscriminatorValueCyclicComposition covers the cyclic half, but a
// walk that never returns fails there as a silent package-wide timeout; this
// fails in seconds and names the cause. Branching, not a cycle, is what
// multiplies the frontier here. It asserts a bound because the defect is work:
// one visit per level with the dedup, 2^level without it.
func TestCompile_SharedCompositionAncestorsDoNotAmplify(t *testing.T) {
	t.Parallel()
	const levels = 30
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    Root:\n      type: object\n" +
		"      properties: {k: {type: string}}\n      discriminator: {propertyName: k}\n")
	prev := "Root"
	for i := range levels {
		fmt.Fprintf(&b, "    S%d:\n      allOf: [{$ref: '#/components/schemas/%s'}, "+
			"{$ref: '#/components/schemas/%s'}]\n      properties: {p%d: {type: string}}\n",
			i, prev, prev, i)
		prev = fmt.Sprintf("S%d", i)
	}

	type result struct {
		doc   *ir.Document
		diags []ir.Diagnostic
		err   error
	}
	done := make(chan result, 1)
	go func() {
		doc, diags, cerr := New().Compile(t.Context(),
			[]compilers.Source{{Path: "shared_ancestors.yaml", Data: []byte(b.String())}},
			compilers.Options{})
		done <- result{doc, diags, cerr}
	}()

	const bound = 10 * time.Second
	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.NotNil(t, r.doc, "a diamond composition is ordinary input, not a refusal")
		leaf, ok := r.doc.Types[ir.TypeID("t/openapi/components/schemas/"+prev)].(*ir.Model)
		require.True(t, ok, "the deepest subtype lowers to a model")
		assert.Equal(t, prev, leaf.DiscriminatorValue,
			"and still answers to the tag its distant ancestor spells for it")
	case <-time.After(bound):
		t.Fatalf("compiling %d levels of shared ancestors did not finish within %v — "+
			"the discriminator ancestor walk is revisiting schemas it has already seen", levels, bound)
	}
}
