package ir_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestUnmodeled_JSONRoundTrip pins that Unmodeled round-trips through
// assertRoundTrip's byte-level cmp.Diff. RawValue is json.RawMessage, which
// json.Marshal compacts and HTML-escapes on the way out, so this only works
// because both fixture values below are already written compact and free of
// <, > and & — either would round-trip to a semantically equal but
// byte-different value and fail the diff.
func TestUnmodeled_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.Unmodeled{
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

// unmodeledReasonWire is the hand-maintained expectation of every reason and
// its serialized form. The strings land in golden IR and in emitter selection
// logic (`Reason == ReasonValidationOnly`), so they are contract rather than
// an internal detail. TestUnmodeledReason_TiesToConstBlock keeps this list from
// drifting behind the const block it mirrors.
var unmodeledReasonWire = map[ir.UnmodeledReason]string{
	ir.ReasonVendorExtension:  "vendor_extension",
	ir.ReasonValidationOnly:   "validation_only",
	ir.ReasonDegradedLowering: "degraded_lowering",
	ir.ReasonNoIRHome:         "no_ir_home",
	ir.ReasonOutOfScope:       "out_of_scope",
}

func TestUnmodeledReason_WireStrings(t *testing.T) {
	t.Parallel()
	for reason, want := range unmodeledReasonWire {
		assert.Equal(t, want, string(reason))
	}
}

// TestUnmodeledReason_TiesToConstBlock closes the gap a bare string enum leaves:
// nothing rejects an undeclared value on deserialization, so Valid is what the
// verifier tests against, and Valid has to stay tied to the const block. It
// parses the ir package with go/ast — the approach
// internal/harness's TestConstBlock_TiesToAnnotationsAndSiteKinds and
// internal/archtest's import rules both use — and enforces two directions: the
// declared reasons and unmodeledReasonWire name the same set, and every declared
// reason is Valid. Adding a constant without teaching Valid about it fails here
// rather than surfacing later as a spurious ir/unknown-unmodeled-reason.
//
// The converse of the second direction is not enforced, and cannot be from here:
// a case added to Valid for a value no constant declares leaves this green,
// because a switch body is not enumerable at run time.
// TestUnmodeledReason_UnknownIsInvalid pins only the two values it names.
//
// Declared is the bar, not written: a reason no compiler has written yet must
// still be Valid, because the wire can carry it the moment one does.
func TestUnmodeledReason_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	declared := parseDeclaredReasons(t)

	want := make([]string, 0, len(unmodeledReasonWire))
	for _, s := range unmodeledReasonWire {
		want = append(want, s)
	}
	require.ElementsMatch(t, want, declared)

	for _, d := range declared {
		assert.True(t, ir.UnmodeledReason(d).Valid(), "declared reason %q must be Valid", d)
	}
}

// TestUnmodeledReason_UnknownIsInvalid pins the other direction: Valid must
// reject a value no const declares, including the zero value a compiler leaves
// behind when it forgets the field.
func TestUnmodeledReason_UnknownIsInvalid(t *testing.T) {
	t.Parallel()
	assert.False(t, ir.UnmodeledReason("totally_invented").Valid())
	assert.False(t, ir.UnmodeledReason("").Valid())
}

// reasonTypeName is the only type spelling a reason declaration may use. Specs
// are matched against it literally rather than bucketed under whatever identifier
// appears, so a same-file alias (`type reason = UnmodeledReason`) cannot join the
// taxonomy at every usage site under a key this test never inspects.
const reasonTypeName = "UnmodeledReason"

// reasonHomeFile returns the base name of the ir source that declares
// reasonTypeName — the reason vocabulary's only home. Every declaration there is
// held to the strict form because the file holds nothing else; in the package's
// other files, only UnmodeledReason-typed declarations are.
//
// It is derived rather than written down. A constant naming the file by hand
// keeps matching nothing once the file is renamed, which disarms the strict check
// without failing anything — the shape this test carried when ir/preserved.go
// became ir/unmodeled.go.
func reasonHomeFile(t *testing.T) string {
	t.Helper()
	var homes []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		if declaresType(t, f, reasonTypeName) {
			homes = append(homes, filepath.Base(path))
		}
	}
	require.Len(t, homes, 1, "exactly one ir source must declare %s, found %v", reasonTypeName, homes)
	return homes[0]
}

// declaresType reports whether f declares a package-level type named name.
func declaresType(t *testing.T, f *ast.File, name string) bool {
	t.Helper()
	for _, decl := range f.Decls {
		gd, isGen := decl.(*ast.GenDecl)
		if !isGen || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, isType := spec.(*ast.TypeSpec)
			require.True(t, isType, "type decl spec is not a TypeSpec: %#v", spec)
			if ts.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// parseDeclaredReasons returns the literal value of every UnmodeledReason-typed
// constant the ir package declares. It parses every production source rather than
// the reason type's own file alone, so a reason declared beside unrelated code is
// recorded instead of missed.
//
// Two spellings stay out of reach, both needing go/types rather than a parse to
// see: an untyped constant in another file (`const legacy = "x"` acquires the
// type only where it is assigned), and one typed through an alias declared in a
// different file. Neither is reachable inside the home file, where every
// declaration must spell the type literally.
func parseDeclaredReasons(t *testing.T) []string {
	t.Helper()
	home := reasonHomeFile(t)
	var out []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			out = append(out, reasonValues(t, gd, filepath.Base(path), home)...)
		}
	}
	require.NotEmpty(t, out, "no UnmodeledReason constants found; the parse went wrong")
	return out
}

// reasonValues returns the unquoted value of each `Name UnmodeledReason =
// "literal"` spec in gd, which was declared in the source file named file. home
// is the reason type's own file, whose every declaration is held to that form.
//
// Nothing is skipped in silence. A spec naming the reason type in any other form
// fails here, and so does any spec at all in home, which holds nothing else:
// skipping would let a constant join the taxonomy at every usage site — untyped,
// built by a conversion, typed through a same-file alias, or held in a mutable
// var — without this test ever recording it, producing exactly the spurious
// ir/unknown-unmodeled-reason the tie exists to prevent.
func reasonValues(t *testing.T, gd *ast.GenDecl, file, home string) []string {
	t.Helper()
	var out []string
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "%s: declaration spec is not a ValueSpec: %#v", file, spec)

		id, isIdent := vs.Type.(*ast.Ident)
		if !isIdent || id.Name != reasonTypeName {
			require.NotEqual(t, home, file,
				"%s in %s declares no %s type; every declaration in this file must spell it "+
					"literally, so an untyped constant, a conversion or a same-file alias cannot "+
					"join the taxonomy unrecorded", vs.Names, file, reasonTypeName)
			continue
		}
		require.Equal(t, token.CONST, gd.Tok,
			"%s in %s: a reason must be a constant; a var of this type is assignable to "+
				"UnmodeledEntry.Reason but need not still hold what this test recorded",
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

func TestUnmodeled_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	p := ir.Unmodeled{
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
