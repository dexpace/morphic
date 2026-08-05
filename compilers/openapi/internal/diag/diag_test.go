package diag_test

import (
	"encoding/json"
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
		diag.CycleScanFailed, diag.OverlayInvalid, diag.OverlayFailed,
		diag.OverlayAction, diag.OverlayOriginIncomplete,
		diag.ValidationOnlyKeyword, diag.FalseSchema,
		diag.NumericPrecision, diag.ExclusiveBoundForm, diag.InvalidStatusKey,
		diag.DegradedConstruct,
		diag.CompositionLowering, diag.DynamicRefExpanded, diag.ConflictingRedecl,
		diag.AliasAmplification, diag.UnattachableRequired, diag.InternalInvariant,
		diag.DuplicateOperationID, diag.UnpreservableConstruct,
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
// declares, read from its own source.
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

// constNamesIn returns how many exported names decl declares as constants.
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
		for _, name := range vs.Names {
			if name.IsExported() {
				n++
			}
		}
	}
	return n
}
