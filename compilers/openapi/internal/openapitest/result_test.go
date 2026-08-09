package openapitest_test

import (
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
func diagAt(code string, sev ir.Severity, pointer string) ir.Diagnostic {
	return ir.Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    "m/" + code,
		Provenance: ir.Provenance{Pointer: pointer},
	}
}

// opNamed builds a service holding one group with the named operations.
func opNamed(names ...string) ir.Service {
	ops := make([]ir.Operation, 0, len(names))
	for _, n := range names {
		ops = append(ops, ir.Operation{Name: ir.Naming{Source: n}})
	}
	return ir.Service{Groups: []ir.OperationGroup{{Operations: ops}}}
}

// TestFindOp_FindsTheOperationBySourceName covers both outcomes: the match, and
// the miss that must fail the test rather than return a zero operation quietly.
func TestFindOp_FindsTheOperationBySourceName(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{opNamed("getA", "getB")}}
	assert.Equal(t, "getB", openapitest.FindOp(t, doc, "getB").Name.Source)

	r := &recorder{}
	assert.Equal(t, ir.Operation{}, openapitest.FindOp(r, doc, "absent"))
	require.Len(t, r.fatalf, 1)
	assert.Contains(t, r.fatalf[0], `operation "absent" not found`)
}

// TestFirstOp_ReturnsTheFirstOperationOfTheFirstGroup pins the shorthand the
// single-operation fixtures use.
func TestFirstOp_ReturnsTheFirstOperationOfTheFirstGroup(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "getA", openapitest.FirstOp(t, opNamed("getA", "getB")).Name.Source)
}

// TestIndexBy_KeysEveryItem covers the generic lookup and PropsByWire, the one
// keying it is used for often enough to have a name.
func TestIndexBy_KeysEveryItem(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[int]string{1: "a", 2: "bb"},
		openapitest.IndexBy([]string{"a", "bb"}, func(s string) int { return len(s) }))

	props := []ir.Property{{WireName: "x"}, {WireName: "y"}}
	byWire := openapitest.PropsByWire(props)
	assert.Equal(t, props[1], byWire["y"])
	assert.Len(t, byWire, 2)
}

// TestRequireNoErrorDiags_FailsOnlyOnAnErrorSeverity pins that warnings pass
// and an error does not, which is the whole contract the suites lean on.
func TestRequireNoErrorDiags_FailsOnlyOnAnErrorSeverity(t *testing.T) {
	t.Parallel()
	openapitest.RequireNoErrorDiags(t, []ir.Diagnostic{diagAt("a", ir.SeverityWarning, "")})

	r := &recorder{}
	openapitest.RequireNoErrorDiags(r, []ir.Diagnostic{diagAt("boom", ir.SeverityError, "")})
	assert.True(t, r.failed(), "an error diagnostic must fail the test")
}

// TestAssertHasCode_RequiresCodeAndSeverityTogether pins that neither half
// matches on its own — a code at the wrong severity is a miss.
func TestAssertHasCode_RequiresCodeAndSeverityTogether(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{diagAt("a", ir.SeverityWarning, "/p")}
	openapitest.AssertHasCode(t, diags, "a", ir.SeverityWarning)

	r := &recorder{}
	openapitest.AssertHasCode(r, diags, "a", ir.SeverityError)
	require.Len(t, r.fatalf, 1)
	assert.Contains(t, r.fatalf[0], `code "a"`)
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

	r := &recorder{}
	openapitest.AssertInfoDiagAt(r, diags, "/q")
	assert.True(t, r.failed(), "no info diagnostic at /q")
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

	r := &recorder{}
	openapitest.AssertProbeDocsKept(r, ir.Docs{Summary: "SUM", Description: "DOC"})
	assert.True(t, r.failed(), "a dropped externalDocs must be reported")
}

// TestAssertProbeExample_ChecksTheSingleExampleValue covers the kept case and
// the dropped case, which returns before reading the value.
func TestAssertProbeExample_ChecksTheSingleExampleValue(t *testing.T) {
	t.Parallel()
	openapitest.AssertProbeExample(t, []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, Str: "abc"}}})

	r := &recorder{}
	openapitest.AssertProbeExample(r, nil)
	assert.True(t, r.failed(), "a dropped example must be reported")
}
