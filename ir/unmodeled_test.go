package ir_test

import (
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestUnmodeled_JSONRoundTrip pins that Unmodeled round-trips through
// assertRoundTrip's byte-level cmp.Diff. Encoding reformats a RawValue on the
// way out, stripping whitespace and making string escapes minimal, so this only
// works because both fixture values below are already in that form; one that is
// not would round-trip to a semantically equal but byte-different value and fail
// the diff.
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

// TestUnmodeledReason_TiesToConstBlock keeps UnmodeledReason.Valid tied to the
// const block, since nothing rejects an undeclared value on deserialization. It
// parses the ir package with go/ast and requires the declared reasons and
// unmodeledReasonWire to name the same set, and every declared reason to be
// Valid: declared is the bar, not written, since the wire can carry a reason
// the moment a compiler first writes it.
//
// The converse, a case added to Valid for a value no constant declares, cannot
// be enforced here, since a switch body is not enumerable at run time.
// TestUnmodeledReason_UnknownIsInvalid pins only the two values it names.
func TestUnmodeledReason_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	declared := parseDeclaredReasons(t)

	want := slices.Collect(maps.Values(unmodeledReasonWire))
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

// reasonTypeName names the reason type, whose constants are the taxonomy.
const reasonTypeName = "UnmodeledReason"

// parseDeclaredReasons returns the value of every UnmodeledReason constant the
// ir package declares, read type-checked: one typed through an alias, built by a
// conversion or written as an expression is recorded as the compiler sees it.
//
// Two guards keep a reason from joining the taxonomy unrecorded. No package-level
// var may have the type, since one is assignable to UnmodeledEntry.Reason without
// still holding what this test read. Every constant in the type's own file must
// have it, so an untyped one written there fails rather than passing as a string.
func parseDeclaredReasons(t *testing.T) []string {
	t.Helper()
	p := irDecls(t)
	named, err := p.Named(reasonTypeName)
	require.NoError(t, err)
	home := p.Fset.Position(named.Obj().Pos()).Filename

	scope := p.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		ofType := types.Identical(types.Unalias(obj.Type()), named)
		switch obj.(type) {
		case *types.Var:
			assert.False(t, ofType, "%s is a var of %s; a reason must be a constant", name, reasonTypeName)
		case *types.Const:
			if p.Fset.Position(obj.Pos()).Filename == home {
				assert.True(t, ofType, "%s in %s is not a %s; every constant in the reason type's file must be one",
					name, filepath.Base(home), reasonTypeName)
			}
		default:
			// A type or a function declares no reason.
		}
	}

	consts, err := p.StringConsts(reasonTypeName)
	require.NoError(t, err)
	require.NotEmpty(t, consts, "no UnmodeledReason constants found; the load went wrong")
	out := make([]string, len(consts))
	for i, c := range consts {
		out[i] = c.Value
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
