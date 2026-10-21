package ir_test

import (
	"encoding/json/v2"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestNewDiagnostic_CoercesMessageToValidUTF8 covers the constructor's message
// contract: valid input is preserved exactly, and each ill-formed byte run is
// replaced by a single U+FFFD. Coercion is what keeps invariant #7 honest for
// diagnostics — see TestNewDiagnostic_RoundTripsByteForByte.
func TestNewDiagnostic_CoercesMessageToValidUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain ascii", in: "no problem here", want: "no problem here"},
		{name: "valid multibyte", in: "café ☕ 音", want: "café ☕ 音"},
		{name: "empty", in: "", want: ""},
		// "\xe0\xa5" is the truncated lead of U+0965 (E0 A5 A5): one ill-formed run.
		{name: "truncated rune", in: "bad \xe0\xa5 here", want: "bad \uFFFD here"},
		// Two separate ill-formed runs → two replacements.
		{name: "two bad runs", in: "\xff a \xfe", want: "\uFFFD a \uFFFD"},
		{name: "trailing bad byte", in: "tail \xc3", want: "tail \uFFFD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := ir.NewDiagnostic(ir.SeverityError, "openapi/validation", tt.in, ir.Provenance{})
			assert.Equal(t, tt.want, d.Message)
			assert.True(t, utf8.ValidString(d.Message), "coerced message must be valid UTF-8")
		})
	}
}

// TestNewDiagnostic_PreservesOtherFields confirms the constructor only touches
// the message: severity, code, and provenance pass through verbatim.
func TestNewDiagnostic_PreservesOtherFields(t *testing.T) {
	t.Parallel()
	prov := ir.Provenance{Source: 2, Pointer: "/paths/~1x", Inferred: "pagination-name-match"}
	d := ir.NewDiagnostic(ir.SeverityWarning, "openapi/degraded-construct", "fine", prov)
	assert.Equal(t, ir.SeverityWarning, d.Severity)
	assert.Equal(t, "openapi/degraded-construct", d.Code)
	assert.Equal(t, "fine", d.Message)
	assert.Equal(t, prov, d.Provenance)
}

// TestNewDiagnostic_RoundTripsByteForByte is the invariant #7 property: a
// constructed diagnostic survives marshal → unmarshal → marshal unchanged.
// Without coercion, marshaling the raw ill-formed bytes would fail outright —
// encoding/json/v2 refuses invalid UTF-8 rather than substituting U+FFFD — so
// the round trip depends entirely on NewDiagnostic having already made
// d.Message valid before either marshal runs.
func TestNewDiagnostic_RoundTripsByteForByte(t *testing.T) {
	t.Parallel()
	d := ir.NewDiagnostic(ir.SeverityError, "openapi/validation", "bad \xe0\xa5 byte", ir.Provenance{})

	first, err := json.Marshal(d)
	require.NoError(t, err)
	var back ir.Diagnostic
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(back)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "re-encoding is byte-for-byte stable")
	assert.Equal(t, d, back, "the in-memory diagnostic survives a JSON round-trip")
}

// TestProvenance_JSONEncodesEachLocatorUnderItsOwnKey pins the wire shape §13
// promises: each kind of locator sits under its own JSON member, so a consumer
// can tell a pointer from a position from a node by the key alone rather than
// by parsing the value. The pointer-only row is byte-identical to what 0.5.0
// wrote — the field was already a string on the wire, so the type change
// underneath it is invisible to a consumer reading JSON.
func TestProvenance_JSONEncodesEachLocatorUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prov ir.Provenance
		want string
	}{
		{
			name: "pointer only",
			prov: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
			want: `{"source":0,"pointer":"/paths/~1x"}`,
		},
		{
			name: "position with line and column",
			prov: ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}},
			want: `{"source":0,"position":{"line":5,"column":1}}`,
		},
		{
			name: "position with line only",
			prov: ir.Provenance{Source: 0, Position: ir.Position{Line: 5}},
			want: `{"source":0,"position":{"line":5}}`,
		},
		{
			name: "node only",
			prov: ir.Provenance{Source: ir.NoSource, Node: "op/x"},
			want: `{"source":-1,"node":"op/x"}`,
		},
		{
			name: "zero value",
			prov: ir.Provenance{},
			want: `{"source":0}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.prov, json.Deterministic(true))
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(raw))

			var back ir.Provenance
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, tt.prov, back, "each locator survives a decode round-trip")
		})
	}
}

// TestSourceInfo_JSONEncodesSelfURI pins SelfURI's wire shape byte for byte: a
// `selfURI` member after the other three when one was declared, and no member
// at all when none was, since empty is the one spelling of none. Each row
// decodes back to the value it encoded.
func TestSourceInfo_JSONEncodesSelfURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		info ir.SourceInfo
		want string
	}{
		{
			name: "a declared $self writes selfURI",
			info: ir.SourceInfo{
				Format:  "openapi@3.2",
				Path:    "openapi.yaml",
				Hash:    "abc123",
				SelfURI: "https://example.com/api/openapi.yaml",
			},
			want: `{"format":"openapi@3.2","path":"openapi.yaml","hash":"abc123",` +
				`"selfURI":"https://example.com/api/openapi.yaml"}`,
		},
		{
			name: "no $self writes no key",
			info: ir.SourceInfo{Format: "openapi@3.2", Path: "openapi.yaml", Hash: "abc123"},
			want: `{"format":"openapi@3.2","path":"openapi.yaml","hash":"abc123"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.info, json.Deterministic(true))
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(raw))

			var back ir.SourceInfo
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, tt.info, back, "the source survives a decode round-trip")
		})
	}
}

// TestFirstError_Cases covers the shapes call sites depend on: no
// diagnostics, diagnostics with no error severity, and diagnostics carrying
// more than one error — where the first, not the last, must be returned.
func TestFirstError_Cases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		diags  []ir.Diagnostic
		want   ir.Diagnostic
		wantOK bool
	}{
		{name: "nil", diags: nil, want: ir.Diagnostic{}, wantOK: false},
		{
			name: "no errors",
			diags: []ir.Diagnostic{
				{Severity: ir.SeverityWarning, Code: "w"},
				{Severity: ir.SeverityInfo, Code: "i"},
			},
			want:   ir.Diagnostic{},
			wantOK: false,
		},
		{
			name: "returns the first error, not the last",
			diags: []ir.Diagnostic{
				{Severity: ir.SeverityWarning, Code: "w"},
				{Severity: ir.SeverityError, Code: "first"},
				{Severity: ir.SeverityError, Code: "second"},
			},
			want:   ir.Diagnostic{Severity: ir.SeverityError, Code: "first"},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.FirstError(tt.diags)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestHasError_Cases covers HasError's boolean-only form over the same shapes
// TestFirstError_Cases exercises, confirming it agrees with FirstError's ok
// value without exposing the matched Diagnostic.
func TestHasError_Cases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		diags []ir.Diagnostic
		want  bool
	}{
		{name: "nil", diags: nil, want: false},
		{name: "no errors", diags: []ir.Diagnostic{{Severity: ir.SeverityWarning}}, want: false},
		{name: "has an error", diags: []ir.Diagnostic{{Severity: ir.SeverityError}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ir.HasError(tt.diags))
		})
	}
}

// TestSourceHash_IsTheLowercaseHexSHA256 pins the spelling against published
// SHA-256 vectors rather than against the standard library it calls, so a
// change of case or encoding fails here before any hash it wrote is compared.
func TestSourceHash_IsTheLowercaseHexSHA256(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", ir.SourceHash(nil))
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", ir.SourceHash([]byte("abc")))
}

// TestMergeDiagnostics_KeepsEachFindingOnceInOrder pins the union both of a
// compiler's channels are read through: the stored list first, then each
// produced finding it does not hold, a repeat within produced kept as it is.
func TestMergeDiagnostics_KeepsEachFindingOnceInOrder(t *testing.T) {
	t.Parallel()
	a := ir.NewDiagnostic(ir.SeverityError, "x/a", "a", ir.Provenance{})
	b := ir.NewDiagnostic(ir.SeverityWarning, "x/b", "b", ir.Provenance{})
	c := ir.NewDiagnostic(ir.SeverityInfo, "x/c", "c", ir.Provenance{Source: 1})

	assert.Equal(t, []ir.Diagnostic{a, b, c}, ir.MergeDiagnostics([]ir.Diagnostic{a, b}, []ir.Diagnostic{b, c}))
	assert.Equal(t, []ir.Diagnostic{c, a}, ir.MergeDiagnostics(nil, []ir.Diagnostic{c, a}))
	stored := []ir.Diagnostic{a}
	assert.Equal(t, stored, ir.MergeDiagnostics(stored, nil), "nothing produced leaves the stored list as it is")
}

// TestMergeDiagnostics_NeverWritesThroughAnAliasedList covers the two lists
// sharing one backing array, as a compiler returning the slice it stored makes
// them: appending to stored in place would overwrite produced before it is read.
// produced opens with a finding stored holds, so a write in place lands on an
// entry that differs from what it writes.
func TestMergeDiagnostics_NeverWritesThroughAnAliasedList(t *testing.T) {
	t.Parallel()
	a := ir.NewDiagnostic(ir.SeverityError, "x/a", "a", ir.Provenance{})
	c := ir.NewDiagnostic(ir.SeverityError, "x/c", "c", ir.Provenance{})
	backing := []ir.Diagnostic{a, a, c}

	merged := ir.MergeDiagnostics(backing[:1], backing[1:])

	assert.Equal(t, []ir.Diagnostic{a, c}, merged)
	assert.Equal(t, []ir.Diagnostic{a, a, c}, backing, "the lists merged from are left as they were")
}

// TestProvenance_InTableAdmitsTheTableAndTheSentinelAlone pins both edges of a
// table and the one sentinel the IR declares: any other negative index is a
// second, undeclared sentinel.
func TestProvenance_InTableAdmitsTheTableAndTheSentinelAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source, n int
		want      bool
	}{
		{0, 1, true},
		{1, 2, true},
		{ir.NoSource, 0, true},
		{ir.NoSource, 3, true},
		{0, 0, false},
		{1, 1, false},
		{ir.NoSource - 1, 3, false},
	} {
		assert.Equal(t, tc.want, ir.Provenance{Source: tc.source}.InTable(tc.n), "source %d of %d", tc.source, tc.n)
	}
}
