package oas_test

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// paramsMissingIn writes n parameter components, each missing its required
// `in` and its schema, so a check reading all of them finds 2n things wrong.
func paramsMissingIn(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  parameters:\n")
	for i := range n {
		fmt.Fprintf(&b, "    P%d: {name: p%d}\n", i, i)
	}
	return b.String()
}

// TestCheck_StopsSilentlyOnceTheContextIsDone pins the check's context, asked
// before each object it reads: cancelled partway, the check reads no further
// object and says nothing of having stopped, since what stopped it is no
// finding about the document, where running out of its budget is.
func TestCheck_StopsSilentlyOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	const n = 50
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(paramsMissingIn(n)), &root))
	check := func(ctx context.Context) ([]ir.Diagnostic, int) {
		read := 0
		diags := oas.NewChecker().Check(ctx, &root, "3.1.0", oas.Options{
			Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
			Seen:   func(oas.Kind, *yaml.Node) { read++ },
		})
		return diags, read
	}
	live, readLive := check(t.Context())
	require.Len(t, live, 2*n, "live, every parameter is read and found wanting")

	ctx := &ctxtest.Countdown{Context: t.Context(), Left: 10}
	stopped, read := check(ctx)

	assert.Less(t, read, readLive)
	assert.LessOrEqual(t, read, 10, "no object is read once the context refuses")
	assert.Less(t, len(stopped), 2*n)
	for _, d := range stopped {
		assert.NotEqual(t, diag.BudgetExceeded, d.Code, "a stop is no budget crossed")
	}
}

// TestCheckFormat_StopsSilentlyOnceTheContextIsDone pins the format check's
// context: a walk it cuts short, the first or the second, ends the check with
// nothing found, rather than with the report of a budget it did not cross.
func TestCheckFormat_StopsSilentlyOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("x-e:\n  ? [k]\n  : 1\n"), &root))
	format := func(ctx context.Context) []ir.Diagnostic {
		return oas.CheckFormat(ctx, &root, "3.1.0", oas.Options{})
	}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: 1 << 30}
	require.NotEmpty(t, format(whole), "live, the key that is no string is found")

	for left := range whole.Calls {
		t.Run(fmt.Sprintf("refused from ask %d", left+1), func(t *testing.T) {
			t.Parallel()

			got := format(&ctxtest.Countdown{Context: t.Context(), Left: left})

			assert.Empty(t, got)
		})
	}
}
