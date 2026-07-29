package ir_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestPreserved_JSONRoundTrip pins that Preserved round-trips through
// assertRoundTrip's byte-level cmp.Diff. RawValue is json.RawMessage, which
// json.Marshal compacts and HTML-escapes on the way out, so this only works
// because both fixture values below are already written compact and free of
// <, > and & — either would round-trip to a semantically equal but
// byte-different value and fail the diff.
func TestPreserved_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.Preserved{
		"openapi:x-rate-limit": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue(`{"limit":100}`),
			Provenance: populatedProvenance(),
		},
		"smithy:aws.api#arn": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue(`"arn:aws:s3"`),
			Provenance: populatedProvenance(),
		},
	})
}

// preserveReasonWire is the hand-maintained expectation of every reason and
// its serialized form. The strings land in golden IR and in emitter selection
// logic (`Reason == ReasonValidationOnly`), so they are contract rather than
// an internal detail. TestPreserveReason_TiesToConstBlock keeps this list from
// drifting behind the const block it mirrors.
var preserveReasonWire = map[ir.PreserveReason]string{
	ir.ReasonVendorExtension:  "vendor_extension",
	ir.ReasonValidationOnly:   "validation_only",
	ir.ReasonDegradedLowering: "degraded_lowering",
	ir.ReasonNoIRHome:         "no_ir_home",
	ir.ReasonOutOfScope:       "out_of_scope",
}

func TestPreserveReason_WireStrings(t *testing.T) {
	t.Parallel()
	for reason, want := range preserveReasonWire {
		assert.Equal(t, want, string(reason))
	}
}

// TestPreserveReason_TiesToConstBlock closes the gap a bare string enum leaves:
// nothing rejects an undeclared value on deserialization, so Valid is what the
// verifier tests against, and Valid has to stay tied to the const block. It
// parses preserved.go with go/ast — the approach
// internal/harness's TestConstBlock_TiesToAnnotationsAndSiteKinds and
// internal/archtest's import rules both use — and requires the declared
// reasons, the table above, and Valid to name the same set. Adding a constant
// without teaching Valid about it fails here rather than surfacing later as a
// spurious ir/unknown-preserve-reason.
//
// Declared is the bar, not written: ReasonOutOfScope reaches no compiler write
// path yet (ir-design §15 excludes Smithy and TypeSpec constructs OpenAPI
// cannot express), and that is a gap in the compilers, not in the enum.
func TestPreserveReason_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	declared := parseDeclaredReasons(t)

	want := make([]string, 0, len(preserveReasonWire))
	for _, s := range preserveReasonWire {
		want = append(want, s)
	}
	require.ElementsMatch(t, want, declared)

	for _, d := range declared {
		assert.True(t, ir.PreserveReason(d).Valid(), "declared reason %q must be Valid", d)
	}
}

// TestPreserveReason_UnknownIsInvalid pins the other direction: Valid must
// reject a value no const declares, including the zero value a compiler leaves
// behind when it forgets the field.
func TestPreserveReason_UnknownIsInvalid(t *testing.T) {
	t.Parallel()
	assert.False(t, ir.PreserveReason("totally_invented").Valid())
	assert.False(t, ir.PreserveReason("").Valid())
}

// parseDeclaredReasons returns the literal value of every PreserveReason-typed
// constant declared in preserved.go, found relative to this test file since
// both live in ir. A constant declared elsewhere in the package, or typed
// through a same-file alias, would not be seen — this file is the only home
// the reason vocabulary has.
func parseDeclaredReasons(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(thisFile), "preserved.go"), nil, 0)
	require.NoError(t, err)

	var out []string
	for _, decl := range f.Decls {
		gd, isConst := decl.(*ast.GenDecl)
		if !isConst || gd.Tok != token.CONST {
			continue
		}
		out = append(out, reasonConstValues(t, gd)...)
	}
	require.NotEmpty(t, out, "no PreserveReason constants found; the parse went wrong")
	return out
}

// reasonConstValues appends the unquoted value of each `Name PreserveReason =
// "literal"` spec in gd, skipping specs of any other type.
func reasonConstValues(t *testing.T, gd *ast.GenDecl) []string {
	t.Helper()
	var out []string
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		if !isValue {
			continue
		}
		if id, isIdent := vs.Type.(*ast.Ident); !isIdent || id.Name != "PreserveReason" {
			continue
		}
		require.Len(t, vs.Values, 1, "reason %v must be declared with one literal value", vs.Names)
		lit, isLit := vs.Values[0].(*ast.BasicLit)
		require.True(t, isLit && lit.Kind == token.STRING, "reason %v must be a string literal", vs.Names)
		unquoted, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		out = append(out, unquoted)
	}
	return out
}

func TestPreserved_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	p := ir.Preserved{
		"graphql:@key":         {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"id"`)},
		"openapi:x-rate-limit": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
		"erlang:opaque":        {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`true`)},
	}
	got := assertDeterministicMarshal(t, p)
	assert.Equal(t,
		`{"erlang:opaque":{"reason":"degraded_lowering","value":true,"provenance":{"source":0}},`+
			`"graphql:@key":{"reason":"vendor_extension","value":"id","provenance":{"source":0}},`+
			`"openapi:x-rate-limit":{"reason":"vendor_extension","value":1,"provenance":{"source":0}}}`,
		got,
	)
}

// TestRawConfig_JSONRoundTrip pins the same contract for RawConfig, which is
// declared protocol configuration rather than an escape hatch and so carries
// no per-entry envelope of its own.
func TestRawConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.RawConfig{
		"groupId":  ir.RawValue(`"orders"`),
		"clientId": ir.RawValue(`"sdk"`),
	})
}

func TestRawConfig_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	got := assertDeterministicMarshal(t, ir.RawConfig{
		"z": ir.RawValue(`1`),
		"m": ir.RawValue(`2`),
		"a": ir.RawValue(`3`),
	})
	assert.Equal(t, `{"a":3,"m":2,"z":1}`, got)
}
