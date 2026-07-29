package ir_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
// parses the ir package with go/ast — the approach
// internal/harness's TestConstBlock_TiesToAnnotationsAndSiteKinds and
// internal/archtest's import rules both use — and enforces two directions: the
// declared reasons and preserveReasonWire name the same set, and every declared
// reason is Valid. Adding a constant without teaching Valid about it fails here
// rather than surfacing later as a spurious ir/unknown-preserve-reason.
//
// The converse of the second direction is not enforced, and cannot be from here:
// a case added to Valid for a value no constant declares leaves this green,
// because a switch body is not enumerable at run time.
// TestPreserveReason_UnknownIsInvalid pins only the two values it names.
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

// reasonTypeName is the only type spelling a reason declaration may use. Specs
// are matched against it literally rather than bucketed under whatever identifier
// appears, so a same-file alias (`type reason = PreserveReason`) cannot join the
// taxonomy at every usage site under a key this test never inspects.
const reasonTypeName = "PreserveReason"

// reasonHomeFile is the reason vocabulary's only home. Every declaration there is
// held to the strict form because the file holds nothing else; in the package's
// other files, only PreserveReason-typed declarations are.
const reasonHomeFile = "preserved.go"

// parseDeclaredReasons returns the literal value of every PreserveReason-typed
// constant the ir package declares. It parses every production source rather than
// preserved.go alone, so a reason declared beside unrelated code is recorded
// instead of missed.
//
// Two spellings stay out of reach, both needing go/types rather than a parse to
// see: an untyped constant in another file (`const legacy = "x"` acquires the
// type only where it is assigned), and one typed through an alias declared in a
// different file. Neither is reachable inside reasonHomeFile, where every
// declaration must spell the type literally.
func parseDeclaredReasons(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			out = append(out, reasonValues(t, gd, filepath.Base(path))...)
		}
	}
	require.NotEmpty(t, out, "no PreserveReason constants found; the parse went wrong")
	return out
}

// reasonValues returns the unquoted value of each `Name PreserveReason =
// "literal"` spec in gd, which was declared in the source file named file.
//
// Nothing is skipped in silence. A spec naming the reason type in any other form
// fails here, and so does any spec at all in reasonHomeFile, which holds nothing
// else: skipping would let a constant join the taxonomy at every usage site —
// untyped, built by a conversion, typed through a same-file alias, or held in a
// mutable var — without this test ever recording it, producing exactly the
// spurious ir/unknown-preserve-reason the tie exists to prevent.
func reasonValues(t *testing.T, gd *ast.GenDecl, file string) []string {
	t.Helper()
	var out []string
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "%s: declaration spec is not a ValueSpec: %#v", file, spec)

		id, isIdent := vs.Type.(*ast.Ident)
		if !isIdent || id.Name != reasonTypeName {
			require.NotEqual(t, reasonHomeFile, file,
				"%s in %s declares no %s type; every declaration in this file must spell it "+
					"literally, so an untyped constant, a conversion or a same-file alias cannot "+
					"join the taxonomy unrecorded", vs.Names, file, reasonTypeName)
			continue
		}
		require.Equal(t, token.CONST, gd.Tok,
			"%s in %s: a reason must be a constant; a var of this type is assignable to "+
				"PreservedEntry.Reason but need not still hold what this test recorded",
			vs.Names, file)

		require.Len(t, vs.Values, 1, "%s in %s: expected exactly one literal value", vs.Names, file)
		lit, isLit := vs.Values[0].(*ast.BasicLit)
		require.True(t, isLit && lit.Kind == token.STRING,
			"%s in %s: value must be a string literal, not a conversion or expression", vs.Names, file)

		unquoted, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		out = append(out, unquoted)
	}
	return out
}

// irSourceFiles lists the ir package's production Go files, located relative to
// this test's own path so the result does not depend on the working directory.
func irSourceFiles(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")

	entries, err := os.ReadDir(filepath.Dir(thisFile))
	require.NoError(t, err)

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(filepath.Dir(thisFile), name))
	}
	require.NotEmpty(t, out, "the ir package must have production sources")
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
