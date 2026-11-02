// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapitest

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// FindOp returns the operation whose source name matches.
func FindOp(t TB, doc *ir.Document, source string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if op.Name.Source == source {
				return op
			}
		}
	}
	t.Fatalf("operation %q not found", source)
	return ir.Operation{}
}

// FirstOp returns the operation at svc.Groups[0].Operations[0], requiring both
// to be non-empty first rather than letting a malformed fixture fail with a bare
// index-out-of-range panic.
func FirstOp(t TB, svc ir.Service) ir.Operation {
	t.Helper()
	require.NotEmpty(t, svc.Groups, "service has no operation groups")
	require.NotEmpty(t, svc.Groups[0].Operations, "first group has no operations")
	return svc.Groups[0].Operations[0]
}

// ParamBySource returns the operation's logical parameter with the given
// source name.
func ParamBySource(op ir.Operation, name string) (ir.Parameter, bool) {
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p, true
		}
	}
	return ir.Parameter{}, false
}

// IndexBy builds a lookup keyed by key(item), the shape behind every
// hand-rolled "m := map[K]T{}; for _, x := range xs { m[key(x)] = x }" loop
// these suites used to repeat per test.
func IndexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}

// PropsByWire indexes a model's properties by wire name.
func PropsByWire(props []ir.Property) map[string]ir.Property {
	return IndexBy(props, func(p ir.Property) string { return p.WireName })
}

// BodyTarget returns the type a single-media-type payload refers to.
//
// The two requires are the reason this is a function rather than the indexing
// expression it wraps: written inline, a payload that is nil or that grew a
// second media type panics on a line that says nothing about which of the two
// happened.
func BodyTarget(t TB, payload *ir.Payload) ir.TypeID {
	t.Helper()
	require.NotNil(t, payload, "the operation declares a body")
	require.Len(t, payload.Contents, 1, "the body declares one media type")
	return payload.Contents[0].Type.Target
}

// NodeConstraints returns the constraints on a Scalar, list, map or tuple, and
// fails on any other node.
func NodeConstraints(t TB, td ir.TypeDef) *ir.Constraints {
	t.Helper()
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints
	case *ir.List:
		return n.Constraints
	case *ir.MapT:
		return n.Constraints
	case *ir.Tuple:
		return n.Constraints
	default:
		t.Fatalf("a %T holds no constraints here", td)
		return nil
	}
}

// RequireNoErrorDiags fails the test if any diagnostic has error severity,
// reporting the first offending diagnostic.
func RequireNoErrorDiags(t TB, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// AssertHasCode requires diags to carry a diagnostic with the given code at the
// given severity.
func AssertHasCode(t TB, diags []ir.Diagnostic, code string, sev ir.Severity) {
	t.Helper()
	if !slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == code && d.Severity == sev }) {
		t.Fatalf("expected a %v diagnostic with code %q, got %+v", sev, code, diags)
	}
}

// HasDiag reports whether diags contains a diagnostic with the exact code, at
// any severity. It is the existential half of the vocabulary: use it where a
// test only needs to know a diagnostic fired, not how many or at what severity.
func HasDiag(diags []ir.Diagnostic, code string) bool {
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == code })
}

// FirstDiag returns the first diagnostic carrying code, at any severity, so a
// test can assert on its provenance or its message.
func FirstDiag(diags []ir.Diagnostic, code string) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == code {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// HasDiagAt reports whether diags contains a diagnostic with the exact code at
// the exact severity.
func HasDiagAt(diags []ir.Diagnostic, code string, sev ir.Severity) bool {
	return CountDiagsAt(diags, code, sev) > 0
}

// HasDiagCodeAt reports whether diags carries code at exactly pointer.
func HasDiagCodeAt(diags []ir.Diagnostic, code, pointer string) bool {
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == code && d.Provenance.Pointer == jsontext.Pointer(pointer)
	})
}

// CountDiagsAt counts the diagnostics in diags matching code and sev exactly.
// code is an exact match with no wildcard: CountDiagsAt(diags, "",
// ir.SeverityError) matches only diagnostics whose code is literally empty — it
// is not a way to spell "every error," and reads dangerously like one, so
// callers who want that must filter on severity alone instead.
func CountDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
}

// PointersOf returns the pointers diags report code at, sorted.
func PointersOf(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Provenance.Pointer)
		}
	}
	slices.Sort(out)
	return out
}

// PreservationClaims returns the messages announcing that something was kept
// under Unmodeled. The unpreservable report is excluded by code rather than by
// wording: it says a construct "could not be kept verbatim under Unmodeled",
// which contains the phrase and is the opposite of a claim.
func PreservationClaims(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if strings.Contains(d.Message, "under Unmodeled") && d.Code != diag.UnpreservableConstruct {
			out = append(out, d.Message)
		}
	}
	return out
}

// DiagMessageAt returns the message of the single diagnostic matching code,
// severity and provenance pointer. Tests that only compare a diagnostic's code
// cannot tell two lowerings apart when both report the same code with different
// reasons, so the reason itself needs an assertable handle.
func DiagMessageAt(t TB, diags []ir.Diagnostic, code string, sev ir.Severity, pointer string) string {
	t.Helper()
	var found []string
	for _, d := range diags {
		if d.Code == code && d.Severity == sev && d.Provenance.Pointer == jsontext.Pointer(pointer) {
			found = append(found, d.Message)
		}
	}
	require.Len(t, found, 1, "want exactly one %v %q at %q, got %+v", sev, code, pointer, diags)
	return found[0]
}

// FirstDegradedWarning returns the first diag.DegradedConstruct warning in
// diags, and whether one was found — the pointer/message inspection counterpart
// to HasDiagAt/CountDiagsAt.
func FirstDegradedWarning(diags []ir.Diagnostic) (ir.Diagnostic, bool) {
	i := slices.IndexFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning
	})
	if i < 0 {
		return ir.Diagnostic{}, false
	}
	return diags[i], true
}

// AssertInfoDiagAt requires one info diagnostic stamped at pointer.
func AssertInfoDiagAt(t TB, diags []ir.Diagnostic, pointer string) {
	t.Helper()
	found := slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Severity == ir.SeverityInfo && d.Provenance.Pointer == jsontext.Pointer(pointer)
	})
	if !found {
		assert.Fail(t, "nothing announced this", "no info diagnostic at %q; got %+v", pointer, diags)
	}
}

// AssertProbeDocsKept checks all three documentation keywords InlineProbeBody
// writes reached d, wherever the position's home turned out to be.
func AssertProbeDocsKept(t TB, d ir.Docs) {
	t.Helper()
	assert.Equal(t, "SUM", d.Summary, "title")
	assert.Equal(t, "DOC", d.Description, "description")
	if assert.Len(t, d.ExternalDocs, 1, "externalDocs") {
		assert.Equal(t, "https://e.example", d.ExternalDocs[0].URL)
		assert.Equal(t, "ED", d.ExternalDocs[0].Description)
	}
}

// AssertProbeExample checks the single example InlineProbeBody writes reached
// the home under test with its value intact.
func AssertProbeExample(t TB, examples []ir.Example) {
	t.Helper()
	if !assert.Len(t, examples, 1, "examples") {
		return
	}
	require.NotNil(t, examples[0].Value)
	assert.Equal(t, "abc", examples[0].Value.Str)
}
