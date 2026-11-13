// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestOracle_EachFindsAnOutcomeOfItsOwn pins the table that names what each
// oracle finds: every oracle finds one outcome, no two the same, none of them
// OutcomeOK, and a name that is no oracle finds none.
func TestOracle_EachFindsAnOutcomeOfItsOwn(t *testing.T) {
	t.Parallel()
	seen := map[Outcome]Oracle{}
	for _, o := range Oracles() {
		outcome, ok := o.finds()
		require.True(t, ok, "%s finds nothing", o)
		assert.NotEqual(t, OutcomeOK, outcome, "%s finds ok", o)
		if other, dup := seen[outcome]; dup {
			t.Errorf("%s and %s both find %s", other, o, outcome)
		}
		seen[outcome] = o
	}
	_, ok := Oracle("no-such-oracle").finds()
	assert.False(t, ok)
}

// TestVerdict_ConstructorsRefuseWhatNoVerdictHolds pins the constructors'
// assertions: a finding needs an oracle that finds something and a detail to
// read, and a decline needs a reason.
func TestVerdict_ConstructorsRefuseWhatNoVerdictHolds(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { found("no-such-oracle", "detail") })
	assert.Panics(t, func() { found(OracleOrder, "") })
	assert.Panics(t, func() { declined(OracleOrder, "") })

	assert.Equal(t, Verdict{Oracle: OracleOrder, Outcome: OutcomeOrderDependent, Detail: "d"}, found(OracleOrder, "d"))
	assert.Equal(t, Verdict{Oracle: OracleOrder, Declined: DeclineTooDeep}, declined(OracleOrder, DeclineTooDeep))
	assert.Equal(t, passed(OracleOrder), OracleOrder.judge("ignored", true))
	assert.Equal(t, found(OracleOrder, "d"), OracleOrder.judge("d", false))
}

// TestCheck_AnErrorDiagnosticStopsNoOracle pins #685: a spec drawing an error
// is held to every later oracle, and what each finds is reported beside the
// error, which stays the result's outcome.
func TestCheck_AnErrorDiagnosticStopsNoOracle(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	errorDiag := []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "stub/broken", "the spec is wrong",
		ir.Provenance{Source: ir.NoSource})}
	compiles := 0
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		compiles++
		doc := soundDoc()
		doc.Diagnostics = errorDiag
		if compiles == 2 { // the determinism oracle's recompile
			doc.Name = "SecondCompile"
		}
		return doc, errorDiag, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))

	assert.Equal(t, OutcomeErrorDiag, r.Outcome, r.Detail)
	assert.Equal(t, "stub/broken: the spec is wrong", r.Detail)
	assert.Equal(t, []Outcome{OutcomeErrorDiag, OutcomeNondeterministic}, foundIn(r))
	assert.Greater(t, compiles, 2, "the oracles after determinism compiled the spec too")
}

// TestCheck_ARefusalDeclinesWhatHoldsADocument pins what a refusal, an error
// and no document, leaves each oracle: those holding a document decline, and
// determinism and order-invariance compare the refusal's findings.
func TestCheck_ARefusalDeclinesWhatHoldsADocument(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "stub/refused", "no",
			ir.Provenance{Source: ir.NoSource})}, nil
	}

	r := Check(context.Background(), "spec", []byte("a: 1\nb: 2\n"))

	assert.Equal(t, []Verdict{
		passed(OracleNoPanic),
		declined(OracleInvariants, DeclineNoDocument),
		found(OracleNoErrorDiagnostic, "stub/refused: no"),
		declined(OracleRoundTrip, DeclineNoDocument),
		passed(OracleDeterminism),
		declined(OracleIdempotence, DeclineNoDocument),
		passed(OracleOrder),
	}, r.Verdicts)
}

// TestCheck_ARefusedReencodingIsDeclined pins what the order oracle is told of
// the source: one that compiles to a document, rewritten to text the compile
// refuses, is the rewrite's failure, which a sweep counts, rather than two
// refusals that compare alike.
func TestCheck_ARefusedReencodingIsDeclined(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	const src = "a:\n  b: 1\n  c: 2\n" // indented otherwise than the encoder indents
	compile = func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		if string(data) == src {
			return soundDoc(), nil, nil
		}
		return nil, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "stub/refused", "no",
			ir.Provenance{Source: ir.NoSource})}, nil
	}

	r := Check(context.Background(), "spec", []byte(src))

	require.Len(t, r.Verdicts, len(Oracles()))
	assert.Equal(t, declined(OracleOrder, DeclineReencodingRefused), r.Verdicts[len(r.Verdicts)-1])
}

// TestCheck_AFaultEndingTheRunLeavesTheRestNotReached pins the verdicts of a
// run that ends early: a panic midway keeps the verdicts given before it, a
// fault before the oracles leaves every one but no-panic not reached, and
// each result still holds one verdict per oracle.
func TestCheck_AFaultEndingTheRunLeavesTheRestNotReached(t *testing.T) {
	notReached := func(os ...Oracle) []Verdict {
		vs := make([]Verdict, 0, len(os))
		for _, o := range os {
			vs = append(vs, declined(o, DeclineNotReached))
		}
		return vs
	}
	t.Run("a panic in the determinism oracle's recompile", func(t *testing.T) {
		orig := compile
		t.Cleanup(func() { compile = orig })
		compiles := 0
		compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
			compiles++
			if compiles > 1 {
				panic("recompile exploded")
			}
			return soundDoc(), nil, nil
		}

		r := Check(context.Background(), "spec", []byte("x"))

		require.Len(t, r.Verdicts, len(Oracles()))
		assert.Equal(t, OutcomePanic, r.Verdicts[0].Outcome)
		assert.Equal(t, r.Verdicts[0].Detail, r.Detail)
		assert.Equal(t, []Verdict{passed(OracleInvariants), passed(OracleNoErrorDiagnostic), passed(OracleRoundTrip)},
			r.Verdicts[1:4])
		assert.Equal(t, notReached(OracleDeterminism, OracleIdempotence, OracleOrder), r.Verdicts[4:])
	})
	t.Run("a Go error from the first compile", func(t *testing.T) {
		orig := compile
		t.Cleanup(func() { compile = orig })
		compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
			return nil, nil, errors.New("compile boom")
		}

		r := Check(context.Background(), "spec", []byte("x"))

		assert.Equal(t, OutcomeError, r.Outcome)
		assert.Equal(t, append([]Verdict{passed(OracleNoPanic)}, notReached(Oracles()[1:]...)...), r.Verdicts)
	})
	t.Run("a stage the verified run refuses", func(t *testing.T) {
		withPasses(t, faulting{first: func(own *ir.Document) *ir.Document {
			own.Types = ir.TypeRegistry{ir.TypeID("t/x/Key"): &ir.Any{ID: "t/x/Other"}}
			return own
		}})

		r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

		require.Len(t, r.Verdicts, len(Oracles()))
		assert.Equal(t, passed(OracleNoPanic), r.Verdicts[0])
		assert.Equal(t, OutcomeViolations, r.Verdicts[1].Outcome, "the engine's refusal is the invariants oracle's")
		assert.Equal(t, notReached(Oracles()[2:]...), r.Verdicts[2:], "the engine returns no document to ask of")
	})
	t.Run("a caller's mistake", func(t *testing.T) {
		r := Check(context.Background(), "", []byte(testspec.Minimal))

		assert.Equal(t, OutcomeError, r.Outcome)
		assert.Equal(t, notReached(Oracles()...), r.Verdicts, "nothing ran, so not even no-panic asked")
	})
}

// TestDeterministic_ComparesARefusalsFindings pins what determinism holds a
// refusal to: the findings it returns in place of a document, which a rerun
// must return again.
func TestDeterministic_ComparesARefusalsFindings(t *testing.T) {
	refusal := func(code string) []ir.Diagnostic {
		return []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, code, "no", ir.Provenance{Source: ir.NoSource})}
	}
	for _, tc := range []struct {
		name      string
		recompile string
		ok        bool
	}{
		{"the same findings", "stub/refused", true},
		{"other findings", "stub/other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := compile
			t.Cleanup(func() { compile = orig })
			compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
				return nil, refusal(tc.recompile), nil
			}

			detail, ok := deterministic(context.Background(), "s", []byte("x"), nil, refusal("stub/refused"))

			assert.Equal(t, tc.ok, ok, detail)
		})
	}
}

// foundIn is what each oracle that found something in r found, in Check's
// order.
func foundIn(r Result) []Outcome {
	var out []Outcome
	for _, v := range r.Verdicts {
		if v.Found() {
			out = append(out, v.Outcome)
		}
	}
	return out
}
