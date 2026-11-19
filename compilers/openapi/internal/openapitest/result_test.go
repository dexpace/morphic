// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapitest_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// recorder is a TB that records failures instead of aborting the run.
//
// Fatalf and FailNow deliberately return rather than calling runtime.Goexit:
// the statements a helper writes after them are unreachable under a real
// *testing.T, so a stub that aborted would leave them uncovered — and they are
// the return values the compiler demands, not dead code.
type recorder struct {
	helpers  int
	errorf   []string
	fatalf   []string
	failNows int
}

func (r *recorder) Helper() { r.helpers++ }

func (r *recorder) Errorf(format string, args ...any) {
	r.errorf = append(r.errorf, fmt.Sprintf(format, args...))
}

func (r *recorder) FailNow() { r.failNows++ }

func (r *recorder) Fatalf(format string, args ...any) {
	r.fatalf = append(r.fatalf, fmt.Sprintf(format, args...))
}

// failed reports whether the helper under test complained through any channel.
func (r *recorder) failed() bool {
	return len(r.errorf) > 0 || len(r.fatalf) > 0
}

// diagAt builds one diagnostic with the code, severity and pointer a case needs.
func diagAt(code string, sev ir.Severity, pointer jsontext.Pointer) ir.Diagnostic {
	return ir.Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    "m/" + code,
		Provenance: ir.Provenance{Pointer: pointer},
	}
}

// operationNamed builds a service holding one group with the named operations.
func operationNamed(names ...string) ir.Service {
	operations := make([]ir.Operation, 0, len(names))
	for _, n := range names {
		operations = append(operations, ir.Operation{Name: ir.Naming{Source: n}})
	}
	return ir.Service{Groups: []ir.OperationGroup{{Operations: operations}}}
}

// TestFindOperation_FindsTheOperationBySourceName covers both outcomes: the match, and
// the miss that must fail the test rather than return a zero operation quietly.
func TestFindOperation_FindsTheOperationBySourceName(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{operationNamed("getA", "getB")}}
	assert.Equal(t, "getB", openapitest.FindOperation(t, doc, "getB").Name.Source)

	recorderValue := &recorder{}
	assert.Equal(t, ir.Operation{}, openapitest.FindOperation(recorderValue, doc, "absent"))
	require.Len(t, recorderValue.fatalf, 1)
	assert.Contains(t, recorderValue.fatalf[0], `operation "absent" not found`)
}

// TestFirstOperation_ReturnsTheFirstOperationOfTheFirstGroup pins the shorthand the
// single-operation fixtures use.
func TestFirstOperation_ReturnsTheFirstOperationOfTheFirstGroup(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "getA", openapitest.FirstOperation(t, operationNamed("getA", "getB")).Name.Source)
}

// TestParamBySource_FindsTheParameterBySourceName covers the match and the
// miss, which reports absence rather than a zero parameter that looks found.
func TestParamBySource_FindsTheParameterBySourceName(t *testing.T) {
	t.Parallel()
	operation := ir.Operation{Params: []ir.Parameter{
		{ID: "property/a", Name: ir.Naming{Source: "a"}},
		{ID: "property/b", Name: ir.Naming{Source: "b"}},
	}}
	parameter, ok := openapitest.ParamBySource(operation, "b")
	require.True(t, ok)
	assert.Equal(t, ir.ParameterID("property/b"), parameter.ID)

	_, ok = openapitest.ParamBySource(operation, "c")
	assert.False(t, ok)
}

// TestIndexBy_KeysEveryItem covers the generic lookup and PropertiesByWire, the one
// keying it is used for often enough to have a name.
func TestIndexBy_KeysEveryItem(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[int]string{1: "a", 2: "bb"},
		openapitest.IndexBy([]string{"a", "bb"}, func(s string) int { return len(s) }))

	properties := []ir.Property{{WireName: "x"}, {WireName: "y"}}
	byWire := openapitest.PropertiesByWire(properties)
	assert.Equal(t, properties[1], byWire["y"])
	assert.Len(t, byWire, 2)
}

// TestBodyTarget_RequiresExactlyOneMediaType pins that a second media type is a
// failure rather than a silent pick of the first.
//
// The recorder can drive this case and not the two that would leave the helper
// nothing to index, a nil payload or a payload with no media type: its FailNow
// returns where a real one aborts, so the helper runs past the guard that fired
// and panics. The passing case executes both of those guards.
func TestBodyTarget_RequiresExactlyOneMediaType(t *testing.T) {
	t.Parallel()
	one := &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "type/primitive/string"}}}}
	assert.Equal(t, ir.TypeID("type/primitive/string"), openapitest.BodyTarget(t, one))

	recorderValue := &recorder{}
	openapitest.BodyTarget(recorderValue, &ir.Payload{Contents: []ir.Content{{}, {}}})
	assert.True(t, recorderValue.failed(), "a second media type must fail the test")
}

// TestRequireNoErrorDiags_FailsOnlyOnAnErrorSeverity pins that warnings pass
// and an error does not, which is the whole contract the suites lean on.
func TestRequireNoErrorDiags_FailsOnlyOnAnErrorSeverity(t *testing.T) {
	t.Parallel()
	openapitest.RequireNoErrorDiags(t, []ir.Diagnostic{diagAt("a", ir.SeverityWarning, "")})

	recorderValue := &recorder{}
	openapitest.RequireNoErrorDiags(recorderValue, []ir.Diagnostic{diagAt("boom", ir.SeverityError, "")})
	assert.True(t, recorderValue.failed(), "an error diagnostic must fail the test")
}

// TestAssertHasCode_RequiresCodeAndSeverityTogether pins that neither half
// matches on its own — a code at the wrong severity is a miss.
func TestAssertHasCode_RequiresCodeAndSeverityTogether(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{diagAt("a", ir.SeverityWarning, "/p")}
	openapitest.AssertHasCode(t, diags, "a", ir.SeverityWarning)

	recorderValue := &recorder{}
	openapitest.AssertHasCode(recorderValue, diags, "a", ir.SeverityError)
	require.Len(t, recorderValue.fatalf, 1)
	assert.Contains(t, recorderValue.fatalf[0], `code "a"`)
}

// TestHasDiag_MatchesCodeAtAnySeverity separates the existential predicate from
// the severity-qualified one beside it.
func TestHasDiag_MatchesCodeAtAnySeverity(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{diagAt("a", ir.SeverityInfo, "/p")}
	assert.True(t, openapitest.HasDiag(diags, "a"))
	assert.False(t, openapitest.HasDiag(diags, "b"))
	assert.True(t, openapitest.HasDiagAt(diags, "a", ir.SeverityInfo))
	assert.False(t, openapitest.HasDiagAt(diags, "a", ir.SeverityError))
}

// TestFirstDiag_ReturnsTheFirstCarryingTheCode pins the order, since a test
// asserts on that one diagnostic's provenance, and the miss.
func TestFirstDiag_ReturnsTheFirstCarryingTheCode(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diagAt("a", ir.SeverityInfo, "/p"),
		diagAt("b", ir.SeverityError, "/q"),
		diagAt("b", ir.SeverityInfo, "/r"),
	}
	diagnostic, ok := openapitest.FirstDiag(diags, "b")
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/q"), diagnostic.Provenance.Pointer)

	_, ok = openapitest.FirstDiag(diags, "c")
	assert.False(t, ok)
}

// TestHasDiagCodeAt_RequiresTheExactPointer pins that the pointer is part of
// the match, not decoration.
func TestHasDiagCodeAt_RequiresTheExactPointer(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{diagAt("a", ir.SeverityInfo, "/p")}
	assert.True(t, openapitest.HasDiagCodeAt(diags, "a", "/p"))
	assert.False(t, openapitest.HasDiagCodeAt(diags, "a", "/q"))
}

// TestCountDiagsAt_CountsExactMatchesOnly guards the documented sharp edge: the
// empty code is a literal, not a wildcard.
func TestCountDiagsAt_CountsExactMatchesOnly(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diagAt("a", ir.SeverityWarning, "/p"),
		diagAt("a", ir.SeverityWarning, "/q"),
		diagAt("a", ir.SeverityError, "/r"),
	}
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, "a", ir.SeverityWarning))
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, "", ir.SeverityWarning))
}

// TestPointersOf_SortsThePointersOfOneCode pins that only the code's pointers
// are listed, and in sorted rather than report order.
func TestPointersOf_SortsThePointersOfOneCode(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diagAt("a", ir.SeverityWarning, "/r"),
		diagAt("b", ir.SeverityWarning, "/q"),
		diagAt("a", ir.SeverityError, "/p"),
	}
	assert.Equal(t, []jsontext.Pointer{"/p", "/r"}, openapitest.PointersOf(diags, "a"))
	assert.Empty(t, openapitest.PointersOf(diags, "c"))
}

// TestPreservationClaims_ExcludesTheUnpreservableReport pins both halves: a
// message claiming a keep is listed whatever its code, and the report that a
// construct could not be kept is not, though its wording holds the phrase.
func TestPreservationClaims_ExcludesTheUnpreservableReport(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		{Code: "a", Message: "x kept under Unmodeled"},
		{Code: diag.UnpreservableConstruct, Message: "y could not be kept verbatim under Unmodeled"},
		{Code: "b", Message: "z reported"},
		{Code: "c", Message: "w kept under Unmodeled"},
	}
	assert.Equal(t, []string{"x kept under Unmodeled", "w kept under Unmodeled"}, openapitest.PreservationClaims(diags))
}

// TestDiagMessageAt_ReturnsTheSingleMatchingMessage pins that the message is
// reachable by code, severity and pointer together.
func TestDiagMessageAt_ReturnsTheSingleMatchingMessage(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diagAt("a", ir.SeverityWarning, "/p"),
		diagAt("a", ir.SeverityWarning, "/q"),
	}
	assert.Equal(t, "m/a", openapitest.DiagMessageAt(t, diags, "a", ir.SeverityWarning, "/q"))
}

// TestFirstDegradedWarning_MatchesTheDegradedCodeAtWarning pins both halves of
// the match: the code it is named for, at warning severity.
func TestFirstDegradedWarning_MatchesTheDegradedCodeAtWarning(t *testing.T) {
	t.Parallel()
	want := diagAt(diag.DegradedConstruct, ir.SeverityWarning, "/p")
	got, ok := openapitest.FirstDegradedWarning([]ir.Diagnostic{
		diagAt(diag.DegradedConstruct, ir.SeverityInfo, "/x"), want,
	})
	require.True(t, ok)
	assert.Equal(t, want, got)

	_, ok = openapitest.FirstDegradedWarning([]ir.Diagnostic{diagAt("other", ir.SeverityWarning, "/p")})
	assert.False(t, ok)
}

// TestAssertInfoDiagAt_MatchesOnSeverityAndPointer pins that this one ignores
// the code — it asks only whether something was announced at that position.
func TestAssertInfoDiagAt_MatchesOnSeverityAndPointer(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{diagAt("any", ir.SeverityInfo, "/p")}
	openapitest.AssertInfoDiagAt(t, diags, "/p")

	recorderValue := &recorder{}
	openapitest.AssertInfoDiagAt(recorderValue, diags, "/q")
	assert.True(t, recorderValue.failed(), "no info diagnostic at /q")
}

// TestAssertProbeDocsKept_ChecksAllThreeDocumentationKeywords covers the kept
// case and the missing-externalDocs case, since the link fields are only read
// when the length assertion holds.
func TestAssertProbeDocsKept_ChecksAllThreeDocumentationKeywords(t *testing.T) {
	t.Parallel()
	kept := ir.Docs{
		Summary:      "SUM",
		Description:  "DOC",
		ExternalDocs: []ir.Link{{URL: "https://e.example", Description: "ED"}},
	}
	openapitest.AssertProbeDocsKept(t, kept)

	recorderValue := &recorder{}
	openapitest.AssertProbeDocsKept(recorderValue, ir.Docs{Summary: "SUM", Description: "DOC"})
	assert.True(t, recorderValue.failed(), "a dropped externalDocs must be reported")
}

// TestAssertProbeExample_ChecksTheSingleExampleValue covers the kept case and
// the dropped case, which returns before reading the value.
func TestAssertProbeExample_ChecksTheSingleExampleValue(t *testing.T) {
	t.Parallel()
	openapitest.AssertProbeExample(t, []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, String: "abc"}}})

	recorderValue := &recorder{}
	openapitest.AssertProbeExample(recorderValue, nil)
	assert.True(t, recorderValue.failed(), "a dropped example must be reported")
}

// TestNodeConstraints_ReadsEachKindThatHoldsThem pins the four kinds a test reads
// bounds off, and the failure on a node that holds none it reads.
func TestNodeConstraints_ReadsEachKindThatHoldsThem(t *testing.T) {
	t.Parallel()
	constraints := &ir.Constraints{}
	for _, td := range []ir.TypeDef{
		&ir.Scalar{Constraints: constraints}, &ir.List{Constraints: constraints}, &ir.MapT{Constraints: constraints}, &ir.Tuple{Constraints: constraints},
	} {
		recorderValue := &recorder{}
		assert.Same(t, constraints, openapitest.NodeConstraints(recorderValue, td), "%T", td)
		assert.False(t, recorderValue.failed(), "%T", td)
	}

	otherRecorder := &recorder{}
	assert.Nil(t, openapitest.NodeConstraints(otherRecorder, &ir.Model{}))
	assert.True(t, otherRecorder.failed(), "a model holds none it reads")
}
