package openapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

var cycleReproducers = []struct{ name, file string }{
	{"self-ref", "cycle_self_ref"},
	{"two-node-ref", "cycle_two_node_ref"},
	{"yaml-anchor", "cycle_yaml_anchor"},
	{"self-ref-sibling", "cycle_self_ref_sibling"},
	{"two-node-ref-sibling", "cycle_two_node_ref_sibling"},
	{"alias-ref-value", "cycle_alias_ref_value"},
	{"content-schema", "cycle_content_schema"},
	{"alias-ref-key", "cycle_alias_ref_key"},
	{"merge-key-ref", "cycle_merge_key_ref"},
	{"alias-schema-node", "cycle_alias_schema_node"},
	{"alias-dual-position", "cycle_alias_dual_position"},
	{"duplicate-key", "cycle_duplicate_key"},
	// Reference-object cycles spelled by document position rather than through
	// components. speakeasy guards the components spelling and faults on these.
	{"path-item-mutual", "cycle_path_item_mutual"},
	{"path-item-self", "cycle_path_item_self"},
	{"webhook-mutual", "cycle_webhook_mutual"},
	{"response-via-path", "cycle_response_via_path"},
	{"path-item-via-component", "cycle_path_item_via_component"},
	// A reference whose pointer passes through a reference already being
	// resolved. Distinct from the cycles above: the hop never completes, so
	// speakeasy's own guard cannot see it and it deadlocks rather than faulting.
	{"path-item-prefix-self", "cycle_path_item_prefix_self"},
	{"path-item-prefix-sibling", "cycle_path_item_prefix_sibling"},
	{"path-item-prefix-chain", "cycle_path_item_prefix_chain"},
	{"component-path-item-prefix", "cycle_component_path_item_prefix"},
	{"webhook-prefix-self", "cycle_webhook_prefix_self"},
	{"path-item-empty-segment", "cycle_path_item_empty_segment"},
	// A self-reference only the resolver's pointer normalization reveals, which
	// overflows the stack rather than deadlocking: the resolution cache ends up
	// pointing at its own reference and GetObject's delegation recurses.
	{"pointer-whitespace-self", "cycle_pointer_whitespace_self"},
}

// TestCycleReproducers_EveryFixtureIsExercised holds cycleReproducers to the
// fixtures on disk, the way TestDanglingRefs_EveryReproducerIsExercised holds
// its own table: a cycle_*.yaml added to the corpus and left out of the table is
// compiled by nothing here, and the suite stays green while the corpus grows
// past it.
//
// The scan package keeps a table of the same fixtures for its own assertion and
// carries the twin of this test. Holding both to one directory is what stops the
// two from drifting apart as well as from the corpus, which neither package can
// check directly — a test package cannot import another's.
func TestCycleReproducers_EveryFixtureIsExercised(t *testing.T) {
	t.Parallel()
	onDisk, err := filepath.Glob(filepath.Join(reproducerDir, "cycle_*.yaml"))
	require.NoError(t, err, "globbing the reproducer corpus")
	require.NotEmpty(t, onDisk, "the corpus holds cycle reproducers")

	listed := make(map[string]bool, len(cycleReproducers))
	for _, tc := range cycleReproducers {
		listed[tc.file+".yaml"] = true
	}
	for _, path := range onDisk {
		assert.True(t, listed[filepath.Base(path)],
			"%s is in %s but not in cycleReproducers", filepath.Base(path), reproducerDir)
	}
	assert.Len(t, cycleReproducers, len(onDisk),
		"cycleReproducers and %s hold different numbers of fixtures", reproducerDir)
}

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
			assertHasErrorCode(t, diags, diag.CyclicRef)
		})
	}
}

var componentOnlyCycles = []struct{ name, data string }{
	{"component-path-items", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/A'}
components:
  pathItems:
    A: {$ref: '#/components/pathItems/B'}
    B: {$ref: '#/components/pathItems/A'}
`},
	{"component-responses", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200": {$ref: '#/components/responses/A'}
components:
  responses:
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/A'}
`},
}

func TestDetectCycles_ComponentOnlyCyclesLeftToResolver(t *testing.T) {
	t.Parallel()
	for _, tc := range componentOnlyCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scan.Cycles(0, scanIndex(t, tc.data)),
				"a components-only cycle is the resolver's to report")

			_, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.name + ".yaml", Data: []byte(tc.data)}}, compilers.Options{})
			require.NoError(t, err)
			assertHasErrorCode(t, diags, diag.UnresolvedRef)
		})
	}
}

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
	// The four cases below are the negative controls for GitHub #26: each uses
	// the same alias/merge/contentSchema shapes as the crashing reproducers, but
	// legally (no degenerate cycle), so the alias- and merge-aware scan must not
	// start refusing valid documents it previously accepted.
	{"legal-anchor-reuse", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {s: &s {type: object}}
components:
  schemas:
    A: *s
    B: {type: object, properties: {p: *s}}
`},
	{"legal-merge-key", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {base: &base {description: base schema}}
components:
  schemas:
    A: {<<: *base, type: object}
`},
	{"legal-content-schema", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {type: string, contentMediaType: application/json, contentSchema: {type: object}}
`},
	{"legal-alias-ref-terminates", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {ref: &r '#/components/schemas/B'}
components:
  schemas:
    A: {$ref: *r}
    B: {type: object}
`},
}

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
				assert.NotEqualf(t, diag.CyclicRef, d.Code, "must not refuse as a cyclic ref: %+v", d)
			}
		})
	}
}

// scanIndex decodes a spec and indexes it, which is what load hands the scan
// once the compile's one decode has run.
func scanIndex(t *testing.T, src string) sourceindex.Index {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	return sourceindex.Build(&root, sourceindex.MaxIndexedNodes)
}

// TestCompile_SchemaEmptyPointerSegmentIsUnresolved pins where reading the
// empty token changes a verdict rather than a hang. Reading '/A/' as stopping
// at A made this shape a cycle; reading it as descending through A makes it
// what it is, a pointer naming a key that is not declared. That moves a schema
// chain from chainCycles to chainReenters, and refCycles refuses a schema chain
// on the first alone, so the document now reaches the resolver.
//
// It reports rather than blocks there: speakeasy resolves a schema $ref as an
// oas3.JSONSchema, and only openapi/reference.go's cacheMutex is held across a
// pointer walk (v1.24.0) — jsonschema/oas3 carries no per-reference lock at all.
func TestCompile_SchemaEmptyPointerSegmentIsUnresolved(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/A/'}
`
	_, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "schema_empty_segment.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	assertHasErrorCode(t, diags, diag.UnresolvedRef)
	for _, d := range diags {
		assert.NotEqualf(t, diag.CyclicRef, d.Code,
			"a pointer that names an undeclared key is unresolved, not cyclic: %+v", d)
	}
}

// reproducerDir is where the cycle fixtures live. The reader below, the table
// guard above and the fuzz seeds all derive their paths from it, so none of them
// can end up looking somewhere the others are not — which matters most for the
// seed loader, since it ignores a read error and a wrong path there would cost
// the fuzzer its seeds in silence.
const reproducerDir = "../../testdata/openapi"

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reproducerDir, file+".yaml"))
	require.NoError(t, err)
	return data
}

func TestCompile_MergeChainPastBoundStillCompiles(t *testing.T) {
	t.Parallel()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "deep-merge.yaml", Data: []byte(ynode.MergeChainSpec(200))}},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "a legal document is still compiled")
	openapitest.AssertHasCode(t, diags, diag.CycleScanFailed, ir.SeverityWarning)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "no diagnostic refuses the source")
	}
}

func FuzzCycleDetector(f *testing.F) {
	for _, tc := range cycleReproducers {
		if data, err := os.ReadFile(filepath.Join(reproducerDir, tc.file+".yaml")); err == nil {
			f.Add(data)
		}
	}
	for _, tc := range refShapedDataSpecs {
		f.Add([]byte(tc.data))
	}
	if data, err := os.ReadFile(amplificationBombFixture); err == nil {
		f.Add(data) // the GitHub #27 reproducer: refused for amplification, not a cycle
	}
	f.Add([]byte(" "))         // whitespace-only: recoverable parser panic
	f.Add([]byte("\t\x00: [")) // undecodable: reported as a parse error, not a fault

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
	})
}

// TestCompile_ReentrantPrefixRefusedInBothDeclarationOrders pins what the
// safe-memo narrowing exists for. Whether a re-entrant hop is seen depends on
// which reference the walk reaches as a chain root first, so a single order
// proves nothing: with a memo keyed on the node alone, declaring the dangling
// reference first records it as terminating and the later chain short-circuits
// straight past the hop. Both orders deadlock in the resolver, so both must be
// refused.
func TestCompile_ReentrantPrefixRefusedInBothDeclarationOrders(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n"
	orders := []struct{ name, body string }{
		{"dangling-declared-first", "  /b: {$ref: '#/paths/~1a/t'}\n  /a: {$ref: '#/paths/~1b'}\n"},
		{"dangling-declared-last", "  /a: {$ref: '#/paths/~1b'}\n  /b: {$ref: '#/paths/~1a/t'}\n"},
	}
	for _, tc := range orders {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: "order.yaml", Data: []byte(head + tc.body)}},
				compilers.Options{})
			require.NoError(t, err, "a re-entrant reference is a spec problem, not a Go error")
			assert.Nil(t, doc, "the compiler refuses a re-entrant reference")
			assertHasErrorCode(t, diags, diag.CyclicRef)
		})
	}
}
