package compilers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// cancellingDetect is a compiler whose Detect cancels the run and answers what
// no detection may: a claim naming no format, which the registry would refuse
// as a breach were it judged, or, when declines is set, a silent decline.
type cancellingDetect struct {
	stubCompiler
	cancel   context.CancelFunc
	declines bool
}

func (c *cancellingDetect) Detect(context.Context, compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	c.cancel()
	return compilers.Recognition{}, nil, !c.declines
}

// TestRegistry_DetectDoesNotJudgeAnAnswerGivenOnceCancelled pins that a
// Detect returning once its context is done fails the detection with the
// context's error, its answer unread: it may have stopped short, so what it
// said is no breach, and no later compiler is asked.
func TestRegistry_DetectDoesNotJudgeAnAnswerGivenOnceCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	first := &cancellingDetect{formats: []compilers.SourceFormat{alpha}, cancel: cancel}
	later := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "alpha"}

	det, ok, err := registryOf(t, first, later).Detect(ctx, compilers.Source{Data: []byte("alpha")}, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, compilers.ErrContract, "an answer given once cancelled is not judged")
	assert.False(t, ok)
	assert.Equal(t, compilers.Detection{}, det)
	assert.Empty(t, later.saw, "no compiler after it is asked")
}

// TestRegistry_DetectDoesNotJudgeADeclineGivenOnceCancelled pins the same for
// a decline by the last compiler asked, which leaves no later turn to find the
// context done: the detection fails with the context's error, rather than
// reporting a source no compiler takes. A Detect the cancellation cut short
// declines in silence, as OpenAPI's does.
func TestRegistry_DetectDoesNotJudgeADeclineGivenOnceCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	last := &cancellingDetect{formats: []compilers.SourceFormat{alpha}, cancel: cancel, declines: true}

	det, ok, err := registryOf(t, last).Detect(ctx, compilers.Source{Data: []byte("alpha")}, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
	assert.Equal(t, compilers.Detection{}, det, "no decline is reported")
}

// cancellingCompile is a compiler whose Compile cancels the run and returns
// what it was scripted to, which breaks the contract.
type cancellingCompile struct {
	*scripted
	cancel context.CancelFunc
}

func (c cancellingCompile) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	c.cancel()
	return c.scripted.Compile(ctx, sources, opts)
}

// TestDetectionCompile_DoesNotJudgeACompileReturningOnceCancelled pins that a
// compile returning once its context is done fails with the context's error
// whatever it returned: a document that breaks the contract, an error of its
// own, or a refusal that gives no reason. Any of them may be what a compile
// cut short leaves.
func TestDetectionCompile_DoesNotJudgeACompileReturningOnceCancelled(t *testing.T) {
	t.Parallel()
	own := errors.New("the compiler's own")
	tests := []struct {
		name  string
		spoil func(*scripted)
	}{
		{name: "an unstamped document", spoil: func(s *scripted) { s.doc.IRVersion = "" }},
		{name: "its own error", spoil: func(s *scripted) { s.err = own }},
		{name: "a refusal giving no reason", spoil: func(s *scripted) { s.doc = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			s := keeping()
			tc.spoil(s)

			got, err := detected(cancellingCompile{scripted: s, cancel: cancel}, nil).Compile(ctx)

			require.ErrorIs(t, err, context.Canceled)
			assert.NotErrorIs(t, err, compilers.ErrContract)
			assert.NotErrorIs(t, err, own)
			assert.Equal(t, compilers.Compiled{}, got)
		})
	}
}
