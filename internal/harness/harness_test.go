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

func TestCheck_NilContextIsErrorNotPanic(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // deliberately passing a nil ctx to exercise the boundary guard.
	r := harness.Check(nil, "minimal", []byte(minimalSpec))
	// A caller mistake is a harness error, never a spec-attributed compiler panic.
	assert.Equal(t, harness.OutcomeError, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, "nil context")
}

func TestCheck_EmptySpecIsErrorNotPanic(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "", []byte(minimalSpec))
	assert.Equal(t, harness.OutcomeError, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, "empty spec")
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
