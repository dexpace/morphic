package openapitest

import (
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
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			return
		}
	}
	t.Fatalf("expected a %v diagnostic with code %q, got %+v", sev, code, diags)
}

// HasDiag reports whether diags contains a diagnostic with the exact code, at
// any severity. It is the existential half of the vocabulary: use it where a
// test only needs to know a diagnostic fired, not how many or at what severity.
func HasDiag(diags []ir.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// HasDiagAt reports whether diags contains a diagnostic with the exact code at
// the exact severity.
func HasDiagAt(diags []ir.Diagnostic, code string, sev ir.Severity) bool {
	return CountDiagsAt(diags, code, sev) > 0
}

// HasDiagCodeAt reports whether diags carries code at exactly pointer.
func HasDiagCodeAt(diags []ir.Diagnostic, code, pointer string) bool {
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == pointer {
			return true
		}
	}
	return false
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

// DiagMessageAt returns the message of the single diagnostic matching code,
// severity and provenance pointer. Tests that only compare a diagnostic's code
// cannot tell two lowerings apart when both report the same code with different
// reasons, so the reason itself needs an assertable handle.
func DiagMessageAt(t TB, diags []ir.Diagnostic, code string, sev ir.Severity, pointer string) string {
	t.Helper()
	var found []string
	for _, d := range diags {
		if d.Code == code && d.Severity == sev && d.Provenance.Pointer == pointer {
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
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// AssertInfoDiagAt requires one info diagnostic stamped at pointer.
func AssertInfoDiagAt(t TB, diags []ir.Diagnostic, pointer string) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && d.Provenance.Pointer == pointer {
			return
		}
	}
	assert.Fail(t, "nothing announced this", "no info diagnostic at %q; got %+v", pointer, diags)
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
