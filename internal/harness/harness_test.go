package harness_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/harness"
)

const minimalSpec = `openapi: 3.0.0
info: {title: t, version: "1"}
paths: {}
`

func TestCheck_ValidSpecIsOK(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "minimal", []byte(minimalSpec))
	assert.Equal(t, harness.OutcomeOK, r.Outcome, r.Detail)
}

func TestCheck_GarbageBytesDoNotPanic(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "garbage", []byte("\x00not a spec:::"))
	// A parse failure is an Error/ErrorDiag outcome, never a panic escaping Check.
	assert.NotEqual(t, harness.OutcomePanic, r.Outcome, r.Detail)
}

func TestReport_IsStableAndSorted(t *testing.T) {
	t.Parallel()
	results := []harness.Result{
		{Spec: "b", Outcome: harness.OutcomeOK},
		{Spec: "a", Outcome: harness.OutcomeError, Detail: "boom"},
	}
	got := harness.Report(results)
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
	assert.Less(t, strings.Index(got, "a"), strings.Index(got, "b"),
		"results sorted by spec name")
}
