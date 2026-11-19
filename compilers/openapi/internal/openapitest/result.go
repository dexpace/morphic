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

// FindOperation returns the operation whose source name matches.
func FindOperation(tb TB, doc *ir.Document, source string) ir.Operation {
	tb.Helper()
	for _, operationGroup := range doc.Services[0].Groups {
		for _, operation := range operationGroup.Operations {
			if operation.Name.Source == source {
				return operation
			}
		}
	}
	tb.Fatalf("operation %q not found", source)
	return ir.Operation{}
}

// FirstOperation returns the operation at svc.Groups[0].Operations[0], requiring both
// to be non-empty first rather than letting a malformed fixture fail with a bare
// index-out-of-range panic.
func FirstOperation(tb TB, svc ir.Service) ir.Operation {
	tb.Helper()
	require.NotEmpty(tb, svc.Groups, "service has no operation groups")
	require.NotEmpty(tb, svc.Groups[0].Operations, "first group has no operations")
	return svc.Groups[0].Operations[0]
}

// ParamBySource returns the operation's logical parameter with the given
// source name.
func ParamBySource(operation ir.Operation, name string) (ir.Parameter, bool) {
	for _, parameter := range operation.Params {
		if parameter.Name.Source == name {
			return parameter, true
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

// PropertiesByWire indexes a model's properties by wire name.
func PropertiesByWire(properties []ir.Property) map[string]ir.Property {
	return IndexBy(properties, func(property ir.Property) string { return property.WireName })
}

// BodyTarget returns the type a single-media-type payload refers to.
//
// The two requires are the reason this is a function rather than the indexing
// expression it wraps: written inline, a payload that is nil or that grew a
// second media type panics on a line that says nothing about which of the two
// happened.
func BodyTarget(tb TB, payload *ir.Payload) ir.TypeID {
	tb.Helper()
	require.NotNil(tb, payload, "the operation declares a body")
	require.Len(tb, payload.Contents, 1, "the body declares one media type")
	return payload.Contents[0].Type.Target
}

// NodeConstraints returns the constraints on a Scalar, list, map or tuple, and
// fails on any other node.
func NodeConstraints(tb TB, td ir.TypeDef) *ir.Constraints {
	tb.Helper()
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
		tb.Fatalf("a %T holds no constraints here", td)
		return nil
	}
}

// RequireNoErrorDiags fails the test if any diagnostic has error severity,
// reporting the first offending diagnostic.
func RequireNoErrorDiags(tb TB, diags []ir.Diagnostic) {
	tb.Helper()
	diagnostic, ok := ir.FirstError(diags)
	require.False(tb, ok, "unexpected error diagnostic: %+v", diagnostic)
}

// AssertHasCode requires diags to carry a diagnostic with the given code at the
// given severity.
func AssertHasCode(tb TB, diags []ir.Diagnostic, code string, sev ir.Severity) {
	tb.Helper()
	if !slices.ContainsFunc(diags, func(diagnostic ir.Diagnostic) bool { return diagnostic.Code == code && diagnostic.Severity == sev }) {
		tb.Fatalf("expected a %v diagnostic with code %q, got %+v", sev, code, diags)
	}
}

// HasDiag reports whether diags contains a diagnostic with the exact code, at
// any severity. It is the existential half of the vocabulary: use it where a
// test only needs to know a diagnostic fired, not how many or at what severity.
func HasDiag(diags []ir.Diagnostic, code string) bool {
	return slices.ContainsFunc(diags, func(diagnostic ir.Diagnostic) bool { return diagnostic.Code == code })
}

// FirstDiag returns the first diagnostic carrying code, at any severity, so a
// test can assert on its provenance or its message.
func FirstDiag(diags []ir.Diagnostic, code string) (ir.Diagnostic, bool) {
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			return diagnostic, true
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
	return slices.ContainsFunc(diags, func(diagnostic ir.Diagnostic) bool {
		return diagnostic.Code == code && diagnostic.Provenance.Pointer == jsontext.Pointer(pointer)
	})
}

// CountDiagsAt counts the diagnostics in diags matching code and sev exactly.
// code is an exact match with no wildcard: CountDiagsAt(diags, "",
// ir.SeverityError) matches only diagnostics whose code is literally empty — it
// is not a way to spell "every error," and reads dangerously like one, so
// callers who want that must filter on severity alone instead.
func CountDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, diagnostic := range diags {
		if diagnostic.Code == code && diagnostic.Severity == sev {
			n++
		}
	}
	return n
}

// PointersOf returns the pointers diags report code at, sorted.
func PointersOf(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, diagnostic.Provenance.Pointer)
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
	for _, diagnostic := range diags {
		if strings.Contains(diagnostic.Message, "under Unmodeled") && diagnostic.Code != diag.UnpreservableConstruct {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// DiagMessageAt returns the message of the single diagnostic matching code,
// severity and provenance pointer. Tests that only compare a diagnostic's code
// cannot tell two lowerings apart when both report the same code with different
// reasons, so the reason itself needs an assertable handle.
func DiagMessageAt(tb TB, diags []ir.Diagnostic, code string, sev ir.Severity, pointer string) string {
	tb.Helper()
	var found []string
	for _, diagnostic := range diags {
		if diagnostic.Code == code && diagnostic.Severity == sev && diagnostic.Provenance.Pointer == jsontext.Pointer(pointer) {
			found = append(found, diagnostic.Message)
		}
	}
	require.Len(tb, found, 1, "want exactly one %v %q at %q, got %+v", sev, code, pointer, diags)
	return found[0]
}

// FirstDegradedWarning returns the first diag.DegradedConstruct warning in
// diags, and whether one was found — the pointer/message inspection counterpart
// to HasDiagAt/CountDiagsAt.
func FirstDegradedWarning(diags []ir.Diagnostic) (ir.Diagnostic, bool) {
	i := slices.IndexFunc(diags, func(diagnostic ir.Diagnostic) bool {
		return diagnostic.Code == diag.DegradedConstruct && diagnostic.Severity == ir.SeverityWarning
	})
	if i < 0 {
		return ir.Diagnostic{}, false
	}
	return diags[i], true
}

// AssertInfoDiagAt requires one info diagnostic stamped at pointer.
func AssertInfoDiagAt(tb TB, diags []ir.Diagnostic, pointer string) {
	tb.Helper()
	found := slices.ContainsFunc(diags, func(diagnostic ir.Diagnostic) bool {
		return diagnostic.Severity == ir.SeverityInfo && diagnostic.Provenance.Pointer == jsontext.Pointer(pointer)
	})
	if !found {
		assert.Fail(tb, "nothing announced this", "no info diagnostic at %q; got %+v", pointer, diags)
	}
}

// AssertProbeDocsKept checks all three documentation keywords InlineProbeBody
// writes reached docs, wherever the position's home turned out to be.
func AssertProbeDocsKept(tb TB, docs ir.Docs) {
	tb.Helper()
	assert.Equal(tb, "SUM", docs.Summary, "title")
	assert.Equal(tb, "DOC", docs.Description, "description")
	if assert.Len(tb, docs.ExternalDocs, 1, "externalDocs") {
		assert.Equal(tb, "https://e.example", docs.ExternalDocs[0].URL)
		assert.Equal(tb, "ED", docs.ExternalDocs[0].Description)
	}
}

// AssertProbeExample checks the single example InlineProbeBody writes reached
// the home under test with its value intact.
func AssertProbeExample(tb TB, examples []ir.Example) {
	tb.Helper()
	if !assert.Len(tb, examples, 1, "examples") {
		return
	}
	require.NotNil(tb, examples[0].Value)
	assert.Equal(tb, "abc", examples[0].Value.String)
}
