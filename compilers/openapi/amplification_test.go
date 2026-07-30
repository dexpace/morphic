package openapi

import (
	"os"
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
