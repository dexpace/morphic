package diag_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestNewf_PopulatesEveryField is the reason this constructor exists: severity,
// code and provenance are always set, and the message is formatted rather than
// taken verbatim. A site that built an ir.Diagnostic by hand could omit any of
// them.
func TestNewf_PopulatesEveryField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sev     ir.Severity
		code    string
		prov    ir.Provenance
		format  string
		args    []any
		wantMsg string
	}{
		{
			name: "a formatted message", sev: ir.SeverityError, code: diag.UnresolvedRef,
			prov:   ir.Provenance{Source: 0, Pointer: "/components/schemas/A"},
			format: "unresolved $ref %q", args: []any{"#/nope"},
			wantMsg: `unresolved $ref "#/nope"`,
		},
		{
			name: "no arguments leaves the format alone", sev: ir.SeverityInfo,
			code: diag.FalseSchema, format: "boolean false schema matches nothing",
			wantMsg: "boolean false schema matches nothing",
		},
		{
			name: "several arguments in order", sev: ir.SeverityWarning,
			code: diag.ConflictingRedecl, format: "%s disagrees with %s at %d",
			args:    []any{"minimum", "exclusiveMinimum", 10},
			wantMsg: "minimum disagrees with exclusiveMinimum at 10",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := diag.Newf(tc.sev, tc.code, tc.prov, tc.format, tc.args...)
			assert.Equal(t, tc.sev, d.Severity)
			assert.Equal(t, tc.code, d.Code)
			assert.Equal(t, tc.prov, d.Provenance)
			assert.Equal(t, tc.wantMsg, d.Message)
		})
	}
}

// TestNewf_SanitizesInvalidUTF8 pins invariant #7 at the compiler boundary. A
// third-party validator that truncates a multibyte rune hands this constructor
// ill-formed bytes, and json.Marshal rewrites those to U+FFFD — so a document
// carrying one would stop round-tripping byte-for-byte. The exhaustive
// constructor contract lives in ir; this proves Newf is wired to it.
func TestNewf_SanitizesInvalidUTF8(t *testing.T) {
	t.Parallel()
	// "\xe0\xa5" is the truncated lead of U+0965 (E0 A5 A5): one ill-formed byte
	// run, coerced to a single U+FFFD.
	d := diag.Newf(ir.SeverityError, diag.Validation, ir.Provenance{}, "bad byte %s here", "\xe0\xa5")
	require.True(t, utf8.ValidString(d.Message), "message must be valid UTF-8")
	assert.Equal(t, "bad byte � here", d.Message, "ill-formed run collapses to one U+FFFD")

	first, err := json.Marshal(d)
	require.NoError(t, err)
	var back ir.Diagnostic
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(back)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "sanitized message round-trips byte-for-byte")
	assert.Equal(t, d.Message, back.Message, "message survives marshal/unmarshal unchanged")
}

// TestHasError_Cases pins the gate the load phase uses to tell a refusal from
// advisory findings it must carry forward.
func TestHasError_Cases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		diags []ir.Diagnostic
		want  bool
	}{
		{name: "no diagnostics is not an error"},
		{
			name: "warnings and hints alone are not an error",
			diags: []ir.Diagnostic{
				{Severity: ir.SeverityWarning}, {Severity: ir.SeverityInfo},
			},
		},
		{
			name: "a single error-severity diagnostic is a refusal",
			diags: []ir.Diagnostic{
				{Severity: ir.SeverityWarning}, {Severity: ir.SeverityError},
			},
			want: true,
		},
		{
			name:  "an error in first position is found too",
			diags: []ir.Diagnostic{{Severity: ir.SeverityError}, {Severity: ir.SeverityInfo}},
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, diag.HasError(tc.diags))
		})
	}
}

// codes lists every code this package declares. It is the fixture the two checks
// below share, and adding a code without adding it here leaves the new code
// unchecked — which TestCodes_MatchTheDeclaredSet catches.
func codes() []string {
	return []string{
		diag.Validation, diag.UnsupportedVersion, diag.UnresolvedRef, diag.CyclicRef,
		diag.CycleScanFailed, diag.SourceTooLarge, diag.UndecodableSource,
		diag.OverlayInvalid, diag.OverlayFailed,
		diag.OverlayAction, diag.OverlayOriginIncomplete,
		diag.ValidationOnlyKeyword, diag.FalseSchema, diag.EmptyEnum,
		diag.NumericPrecision, diag.ExclusiveBoundForm, diag.InvalidStatusKey,
		diag.DuplicateStatusKey,
		diag.InvalidMethodKey, diag.DegradedConstruct,
		diag.CompositionLowering, diag.DynamicRefExpanded, diag.ConflictingRedecl,
		diag.DisjointVisibility,
		diag.AliasAmplification, diag.BudgetExceeded,
		diag.UnattachableRequired, diag.InternalInvariant,
		diag.DuplicateOperationID, diag.IncompleteSecurityScheme,
		diag.ReservedHeaderName, diag.UnpreservableConstruct,
		diag.UnknownSchemaKeyword, diag.UnknownObjectKey, diag.UnknownKeyBudget,
		diag.UnknownKeyUnreachable, diag.UnknownKeyEntryTaken,
	}
}

// TestCodes_AreNamespacedAndDistinct pins what makes a code usable by a consumer
// that allowlists them (ir-design §13): each is namespaced to this format, and
// no two share a spelling. A duplicate would make two unrelated findings
// indistinguishable to anything filtering on the code, and the constants
// themselves cannot collide in a way the compiler would notice.
func TestCodes_AreNamespacedAndDistinct(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, len(codes()))
	for _, code := range codes() {
		assert.True(t, strings.HasPrefix(code, "openapi/"),
			"%q must be namespaced to the format that emits it", code)
		assert.NotEqual(t, "openapi/", code, "%q carries no name after its namespace", code)
		assert.False(t, seen[code], "two codes share the spelling %q", code)
		seen[code] = true
	}
}

// TestCodes_MatchTheDeclaredSet guards the fixture above against rot. It counts
// the exported string constants the package declares and requires codes() to
// list exactly that many, so a code added to the package but not to the fixture
// fails here instead of going unchecked by the two tests that read it.
func TestCodes_MatchTheDeclaredSet(t *testing.T) {
	t.Parallel()
	assert.Len(t, codes(), declaredCodeCount(t),
		"codes() must list every code the package declares")
}

// declaredCodeCount returns how many exported string constants the package
// declares, read from its own source. A code is a string, so an exported
// constant of any other kind — MaxQuotedErrorBytes is one — is not one and is
// not counted; the kind is read off the declaration rather than the name, so a
// code added here is counted whatever it is called.
//
// It is parsed rather than written down because a maintained count is exactly
// the claim that rots silently: a code added without touching this file would
// leave the number right by accident until it was not. Reading the declarations
// makes the fixture's completeness a property of the source.
func declaredCodeCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var n, parsed int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, parseErr)
		parsed++
		for _, decl := range file.Decls {
			n += constNamesIn(decl)
		}
	}
	require.Positive(t, parsed, "found no non-test source to read the codes from")
	require.Positive(t, n, "found no declared codes to count")
	return n
}

// constNamesIn returns how many exported names decl declares as string
// constants.
func constNamesIn(decl ast.Decl) int {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return 0
	}
	var n int
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if name.IsExported() && isStringLiteral(vs, i) {
				n++
			}
		}
	}
	return n
}

// isStringLiteral reports whether the i'th name of vs is bound to a string
// literal. A ValueSpec with no values at position i is an iota-style or repeated
// declaration, which no code in this package uses and which names no string.
func isStringLiteral(vs *ast.ValueSpec, i int) bool {
	if i >= len(vs.Values) {
		return false
	}
	lit, ok := vs.Values[i].(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// TestOneLine_CollapsesWhatALibraryWrote pins both join rules and the reason for
// each: a flat list of findings reads as a list, while a header that ends in a
// colon owns the line after it and must not be cut from it by a semicolon.
func TestOneLine_CollapsesWhatALibraryWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"already one line", "plain failure", "plain failure"},
		{"flat list", "first problem\nsecond problem", "first problem; second problem"},
		{"header and items", "yaml: unmarshal errors:\n  line 1: bad\n  line 2: worse",
			"yaml: unmarshal errors: line 1: bad; line 2: worse"},
		{"blank lines dropped", "one\n\n\ntwo", "one; two"},
		{"indentation normalized", "one\n\t  two   three", "one; two three"},
		{"trailing newline", "only\n", "only"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := diag.OneLine(errors.New(tc.in))
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "\n", "the whole point is that nothing survives as a newline")
		})
	}
}

// TestOneLine_BoundsWhatALibraryWrote pins the cap. A diagnostic message is
// something a person reads and something a log stores, and neither survives an
// unbounded one: yaml.v3 reports a duplicated mapping key once per prior
// occurrence, so a 32 KB source with one key repeated 6,553 times produces a
// 1.2 GB error string, which this used to copy whole into a message the CLI
// then printed.
func TestOneLine_BoundsWhatALibraryWrote(t *testing.T) {
	t.Parallel()
	huge := errors.New(strings.Repeat("a line of complaint\n", 1<<16))
	got := diag.OneLine(huge)

	assert.Less(t, len(got), diag.MaxQuotedErrorBytes+64,
		"a foreign error may be any size; what it contributes to a message may not")
	assert.True(t, strings.HasPrefix(got, "a line of complaint; a line of complaint"),
		"the cut keeps the head, which is the part that says what went wrong")
	assert.Contains(t, got, "elided", "a cut message says it was cut")
}

// TestOneLine_CutsOnARuneBoundary holds the cut to well-formed output. The bytes
// being quoted are a foreign library's and may be multi-byte; cutting one in
// half would put ill-formed UTF-8 into a diagnostic, which is the one thing a
// report must never do to a reader.
func TestOneLine_CutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	for pad := range 8 {
		got := diag.OneLine(errors.New(strings.Repeat("x", pad) + strings.Repeat("é", diag.MaxQuotedErrorBytes)))
		assert.True(t, utf8.ValidString(got), "pad %d: a cut message is still text", pad)
	}
}

// TestOneLine_IsBoundedInWorkNotOnlyOutput holds the cap to being a bound on
// work. A message capped by collapsing the whole error and trimming the result
// still walks the whole error, which is the half that costs the time: the 1.2 GB
// case spent 7.4 s building the parts it was about to throw away.
//
// Allocation count is the probe because the per-line work is what allocates —
// one strings.Fields join per line — so a scan that stops at the cap allocates
// the same for two errors that both exceed it, and one that does not scales with
// the error. It is not run in parallel: AllocsPerRun measures the process.
func TestOneLine_IsBoundedInWorkNotOnlyOutput(t *testing.T) {
	small := errors.New(strings.Repeat("line\n", 1<<10))
	large := errors.New(strings.Repeat("line\n", 1<<20))
	require.Greater(t, len(small.Error()), diag.MaxQuotedErrorBytes,
		"both inputs must exceed the cap, or the comparison is between two uncapped runs")

	assert.Equal(t, diag.OneLine(small), diag.OneLine(large),
		"past the cap the answer no longer depends on how much more there was")
	assert.Equal(t,
		testing.AllocsPerRun(2, func() { _ = diag.OneLine(small) }),
		testing.AllocsPerRun(2, func() { _ = diag.OneLine(large) }),
		"past the cap the work no longer depends on how much more there was")
}
