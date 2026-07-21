package openapi

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// cycleReproducers are documents from GitHub #12 whose schema graph cycles never
// reach a node without a top-level $ref. Each crashed the process with a fatal,
// unrecoverable stack overflow before the pre-parse detector was added. The
// sibling variants carry a concrete `type` alongside the $ref: speakeasy follows
// the top-level $ref regardless of the sibling, so they crash exactly like the
// bare-$ref forms and must be diagnosed the same way.
var cycleReproducers = []struct{ name, file string }{
	{"self-ref", "cycle_self_ref"},
	{"two-node-ref", "cycle_two_node_ref"},
	{"yaml-anchor", "cycle_yaml_anchor"},
	{"self-ref-sibling", "cycle_self_ref_sibling"},
	{"two-node-ref-sibling", "cycle_two_node_ref_sibling"},
}

// TestDetectCycles_Reproducers pins that each degenerate cycle is diagnosed as an
// error with line:col provenance, directly at the detector boundary.
func TestDetectCycles_Reproducers(t *testing.T) {
	t.Parallel()
	for _, tc := range cycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			diags := detectCycles(0, data)
			require.NotEmpty(t, diags, "degenerate cycle must be diagnosed")
			assert.Equal(t, codeCyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.NotEmpty(t, diags[0].Provenance.Pointer, "line:col provenance")
		})
	}
}

// TestCompile_CyclicSpecDoesNotCrash drives each degenerate spec through the full
// public Compile path: it must surface an error diagnostic and a nil document
// rather than crashing the process with a fatal stack overflow (GitHub #12), and
// it must not report a Go error — a cyclic spec is a spec problem, not I/O.
func TestCompile_CyclicSpecDoesNotCrash(t *testing.T) {
	t.Parallel()
	for _, tc := range cycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			doc, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: data}}, compilers.Options{})
			require.NoError(t, err, "cyclic spec is a spec problem, not a Go error")
			assert.Nil(t, doc, "the compiler refuses to lower a cyclic spec")
			assertHasErrorCode(t, diags, codeCyclicRef)
		})
	}
}

// TestDetectCycles_LegalRecursionClean is the control: a legal recursive schema
// (a concrete node whose property $refs itself) has a concrete node in the cycle
// and must not be flagged.
func TestDetectCycles_LegalRecursionClean(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/conformance/openapi/recursive.yaml")
	require.NoError(t, err)
	assert.Empty(t, detectCycles(0, data), "legal recursion is not a degenerate cycle")
}

// refShapedDataSpecs are legal documents that speakeasy compiles without a
// crash: a $ref-shaped mapping inside example/default data or an x-* extension
// is opaque data (never resolved), and a pure-$ref cycle among non-schema
// reference objects is caught by speakeasy's own resolver guard. The pre-parse
// detector must leave all of them alone rather than refuse a valid source.
var refShapedDataSpecs = []struct {
	name string
	data string
}{
	{"example-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      example:
        p: {$ref: '#/components/schemas/A/example/q'}
        q: {$ref: '#/components/schemas/A/example/p'}
`},
	{"default-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      default:
        p: {$ref: '#/components/schemas/A/default/q'}
        q: {$ref: '#/components/schemas/A/default/p'}
`},
	{"extension-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-foo: {a: {$ref: '#/x-foo/b'}, b: {$ref: '#/x-foo/a'}}
`},
	{"responses-ref-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  responses:
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/A'}
`},
	{"allof-wrapped-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}]}
    B: {allOf: [{$ref: '#/components/schemas/A'}]}
`},
}

// TestDetectCycles_RefShapedDataIsClean pins that the detector does not fire on a
// $ref-shaped structure outside a schema position: those never reach the
// crashing schema resolver, so flagging them would refuse a legal document.
func TestDetectCycles_RefShapedDataIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, detectCycles(0, []byte(tc.data)),
				"a ref-shaped structure outside a schema position is not a degenerate cycle")
		})
	}
}

// TestCompile_RefShapedDataNotRefused drives the same legal documents through the
// full public Compile path: the compiler must lower them (non-nil document) and
// must not raise a cyclic-ref diagnostic, since speakeasy handles them cleanly.
func TestCompile_RefShapedDataNotRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.name + ".yaml", Data: []byte(tc.data)}}, compilers.Options{})
			require.NoError(t, err)
			assert.NotNil(t, doc, "a legal ref-shaped-data spec must still lower")
			for _, d := range diags {
				assert.NotEqualf(t, codeCyclicRef, d.Code, "must not refuse as a cyclic ref: %+v", d)
			}
		})
	}
}

// TestDetectCycles_NonYAMLIsNoCycle pins that undecodable input yields no cycle
// diagnostics: the main parser owns reporting a parse problem.
func TestDetectCycles_NonYAMLIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, detectCycles(0, nil))
	assert.Empty(t, detectCycles(0, []byte("\t\x00: [")))
}

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/openapi/" + file + ".yaml")
	require.NoError(t, err)
	return data
}

func assertHasErrorCode(t *testing.T, diags []ir.Diagnostic, code string) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			return
		}
	}
	t.Fatalf("expected an error diagnostic with code %q, got %+v", code, diags)
}
