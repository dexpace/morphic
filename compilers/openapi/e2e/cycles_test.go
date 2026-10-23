package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// aliasCycleReproducers are the cycle fixtures whose loop is a YAML anchor
// naming its own ancestor, with the alias the compile refuses it at. The parser
// cannot decode one, so the source is refused and no document comes back.
var aliasCycleReproducers = []struct{ name, file, at string }{
	{"yaml-anchor", "cycle_yaml_anchor", "/x-a/0"},
}

// schemaRefCycles are the cycle fixtures whose loop is made of schema $refs,
// with the one site each loop is reported at: its member with the least pointer
// in the source. The compiler resolves schema $refs itself, so the loop is
// reported without refusing the document, and its members lower to any.
var schemaRefCycles = []struct{ name, file, at string }{
	{"self-ref", "cycle_self_ref", "/components/schemas/A"},
	{"two-node-ref", "cycle_two_node_ref", "/components/schemas/A"},
	{"self-ref-sibling", "cycle_self_ref_sibling", "/components/schemas/A"},
	{"two-node-ref-sibling", "cycle_two_node_ref_sibling", "/components/schemas/A"},
	{"alias-ref-value", "cycle_alias_ref_value", "/components/schemas/A"},
	{"content-schema", "cycle_content_schema", "/components/schemas/B/contentSchema"},
	{"alias-ref-key", "cycle_alias_ref_key", "/components/schemas/A"},
	{"merge-key-ref", "cycle_merge_key_ref", "/components/schemas/A"},
	{"alias-schema-node", "cycle_alias_schema_node", "/components/schemas/A"},
	{"alias-dual-position", "cycle_alias_dual_position", "/components/schemas/B"},
	{"duplicate-key", "cycle_duplicate_key", "/components/schemas/B/contentSchema"},
	// A self-reference only the pointer's whitespace trim reveals.
	{"pointer-whitespace-self", "cycle_pointer_whitespace_self", "/components/schemas/A"},
	// A self-reference only a plus read as a plus reveals (GitHub #911).
	{"pointer-plus-self", "cycle_pointer_plus_self", "/x-a+b"},
	// A loop closed by a pointer passing back through a schema $ref on it
	// (GitHub #932).
	{"schema-through-own-member", "cycle_schema_through_own_member", "/components/schemas/A"},
	{"schema-through-member-chain", "cycle_schema_through_member_chain", "/components/schemas/A"},
	// A loop through the member of a map of schemas keyed "".
	{"schema-empty-key", "cycle_schema_empty_key", "/components/schemas/"},
	// A loop under a default response, a schema position (GitHub #949).
	{"schema-default-response", "cycle_schema_default_response", "/paths/~1a/get/responses/default/content/application~1json/schema"},
}

// unreadSchemaRefs are cycle fixtures whose $ref names nothing, so there is no
// loop to report: the $ref is reported unresolved at the site given instead.
// A pointer ending in an empty token names the key "" of what it passes, and a
// schema's member u has none.
var unreadSchemaRefs = []struct{ name, file, at string }{
	{"schema-empty-token-stays", "cycle_schema_empty_token_stays", "/components/schemas/A"},
}

// referenceObjectCycles are chains of reference objects that cycle, or whose
// pointer passes back through a reference on the chain, with a reference each
// is reported unresolved at. The resolver follows such a chain in a loop and
// reports it, so the compile reaches the lowering (GitHub #574).
var referenceObjectCycles = []struct {
	name, file string
	at         string
}{
	{"path-item-mutual", "cycle_path_item_mutual", "/paths/~1a"},
	{"path-item-self", "cycle_path_item_self", "/paths/~1a"},
	{"webhook-mutual", "cycle_webhook_mutual", "/webhooks/onA"},
	{"response-via-path", "cycle_response_via_path", "/paths/~1a/get/responses/200"},
	{"path-item-via-component", "cycle_path_item_via_component", "/paths/~1a"},
	{"path-item-prefix-self", "cycle_path_item_prefix_self", "/paths/~1a"},
	{"path-item-prefix-sibling", "cycle_path_item_prefix_sibling", "/paths/~1a"},
	{"path-item-prefix-chain", "cycle_path_item_prefix_chain", "/paths/~1b"},
	{"component-path-item-prefix", "cycle_component_path_item_prefix", "/components/pathItems/A"},
	{"webhook-prefix-self", "cycle_webhook_prefix_self", "/webhooks/onA"},
	{"path-item-empty-segment", "cycle_path_item_empty_segment", "/components/pathItems/A"},
}

// TestCycleReproducers_EveryFixtureIsExercised holds the four fixture tables
// above to the fixtures on disk, the way
// TestDanglingRefs_EveryReproducerIsExercised holds its own table: a
// cycle_*.yaml added to the corpus and left out of all is compiled by nothing
// here, and the suite stays green while the corpus grows past it.
//
// The scan package keeps tables of the same fixtures for its own assertion and
// carries the twin of this test. Holding both to one directory is what stops the
// two from drifting apart as well as from the corpus, which neither package can
// check directly — a test package cannot import another's.
func TestCycleReproducers_EveryFixtureIsExercised(t *testing.T) {
	t.Parallel()
	onDisk, err := filepath.Glob(filepath.Join(reproducerDir, "cycle_*.yaml"))
	require.NoError(t, err, "globbing the reproducer corpus")
	require.NotEmpty(t, onDisk, "the corpus holds cycle reproducers")

	listed := make(map[string]bool, len(onDisk))
	for _, file := range cycleFixtureFiles() {
		require.False(t, listed[file+".yaml"], "%s is in more than one table", file)
		listed[file+".yaml"] = true
	}
	for _, path := range onDisk {
		assert.True(t, listed[filepath.Base(path)],
			"%s is in %s but in none of the tables", filepath.Base(path), reproducerDir)
	}
	assert.Len(t, listed, len(onDisk), "the tables and %s hold different numbers of fixtures", reproducerDir)
}

// cycleFixtureFiles is every fixture the tables above name, for the guard
// against the corpus growing past them and for the fuzzer's seeds.
func cycleFixtureFiles() []string {
	files := make([]string, 0, len(aliasCycleReproducers)+len(schemaRefCycles)+len(unreadSchemaRefs)+
		len(referenceObjectCycles))
	for _, tc := range aliasCycleReproducers {
		files = append(files, tc.file)
	}
	for _, tc := range schemaRefCycles {
		files = append(files, tc.file)
	}
	for _, tc := range unreadSchemaRefs {
		files = append(files, tc.file)
	}
	for _, tc := range referenceObjectCycles {
		files = append(files, tc.file)
	}
	return files
}

// cyclicRefSites returns the pointers diags report a cyclic $ref at, in order.
func cyclicRefSites(diags []ir.Diagnostic) []string {
	var sites []string
	for _, d := range diags {
		if d.Code == diag.CyclicRef {
			sites = append(sites, string(d.Provenance.Pointer))
		}
	}
	return sites
}

// TestCompile_AliasCycleIsRefused pins what still refuses a source: a YAML
// anchor naming its own ancestor, which the parser cannot decode. The compile
// returns no document, one cyclic-ref error at the alias, and no Go error, since
// a hostile spec is a spec problem.
func TestCompile_AliasCycleIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range aliasCycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: readReproducer(t, tc.file)}}, compilers.Options{})
			require.NoError(t, err, "a recursive anchor is a spec problem, not a Go error")
			assert.Nil(t, doc, "the compiler refuses to read a source whose aliases never end")
			assertHasErrorCode(t, diags, diag.CyclicRef)
			assert.Equal(t, []string{tc.at}, cyclicRefSites(diags), "the alias is named once")
		})
	}
}

// TestCompile_SchemaRefCycleIsReportedAndStillCompiles pins what a loop of
// schema $refs does under the compiler's own resolution, in every spelling the
// corpus holds. The loop is reported once, as an error at its member with the
// least pointer, and the document is still returned.
func TestCompile_SchemaRefCycleIsReportedAndStillCompiles(t *testing.T) {
	t.Parallel()
	for _, tc := range schemaRefCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: readReproducer(t, tc.file)}}, compilers.Options{})
			require.NoError(t, err, "a cyclic spec is a spec problem, not a Go error")
			assert.NotNil(t, doc, "a loop of schema $refs does not refuse the document")
			assert.True(t, openapitest.HasDiagAt(diags, diag.CyclicRef, ir.SeverityError),
				"the loop is an error; got %+v", diags)
			assert.Equal(t, []string{tc.at}, cyclicRefSites(diags), "the loop is reported once, at its least member")
		})
	}
}

// TestCompile_SchemaRefNamingNothingIsUnresolvedNotCyclic pins the fixtures
// that look like loops and are not: the $ref names a member the document does
// not hold, so it is reported unresolved where it is written and no cycle is
// reported at all.
func TestCompile_SchemaRefNamingNothingIsUnresolvedNotCyclic(t *testing.T) {
	t.Parallel()
	for _, tc := range unreadSchemaRefs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: readReproducer(t, tc.file)}}, compilers.Options{})
			require.NoError(t, err)
			assert.NotNil(t, doc)
			assert.Empty(t, cyclicRefSites(diags), "there is no loop to report")
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, tc.at),
				"the reference is reported unresolved at %s; got %+v", tc.at, diags)
		})
	}
}

// TestCompile_CycleIsReportedAtItsLeastMemberInEitherOrder pins where a loop is
// reported: once per loop, at the loop's member with the least pointer, never
// at a $ref that only leads into it, and wherever the members are declared
// (GitHub #912). A member of a loop lowers to any, and what only leads into
// one stays an alias to it.
func TestCompile_CycleIsReportedAtItsLeastMemberInEitherOrder(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"
	tests := []struct {
		name    string
		members []string
		sites   []string
		anys    []string // the members that lower to any
		alias   string   // a member that only leads in, and what it aliases
		target  string
	}{
		{"a reference into a self-loop", []string{
			"    R: {$ref: '#/components/schemas/A'}\n",
			"    A: {$ref: '#/components/schemas/A'}\n",
		}, []string{"/components/schemas/A"}, []string{"A"}, "R", "A"},
		{"two self-loops", []string{
			"    Z: {$ref: '#/components/schemas/Z'}\n",
			"    Y: {$ref: '#/components/schemas/Y'}\n",
		}, []string{"/components/schemas/Y", "/components/schemas/Z"}, []string{"Y", "Z"}, "", ""},
		{"a loop of three", []string{
			"    C: {$ref: '#/components/schemas/A'}\n",
			"    B: {$ref: '#/components/schemas/C'}\n",
			"    A: {$ref: '#/components/schemas/B'}\n",
		}, []string{"/components/schemas/A"}, []string{"A", "B", "C"}, "", ""},
	}
	for _, tc := range tests {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%t", tc.name, reversed), func(t *testing.T) {
				t.Parallel()
				members := slices.Clone(tc.members)
				if reversed {
					slices.Reverse(members)
				}

				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: "loop.yaml", Data: []byte(head + strings.Join(members, ""))}},
					compilers.Options{})

				require.NoError(t, err)
				require.NotNil(t, doc)
				assert.Equal(t, tc.sites, slices.Sorted(slices.Values(cyclicRefSites(diags))), "got %+v", diags)
				for _, name := range tc.anys {
					scalar, ok := typeByName(doc, name).(*ir.Scalar)
					require.True(t, ok, "%s is a scalar, got %T", name, typeByName(doc, name))
					require.NotNil(t, scalar.Base)
					assert.Equal(t, ir.PrimTypeID(ir.PrimAny), scalar.Base.Target, "%s lowers to any", name)
				}
				if tc.alias != "" {
					scalar, ok := typeByName(doc, tc.alias).(*ir.Scalar)
					require.True(t, ok, "%s is a scalar, got %T", tc.alias, typeByName(doc, tc.alias))
					require.NotNil(t, scalar.Base)
					assert.Equal(t, componentID(tc.target), scalar.Base.Target, "%s stays an alias to %s", tc.alias, tc.target)
				}
			})
		}
	}
}

// TestCompile_ALoopThroughAnotherDocumentIsReportedOnce pins a loop of schema
// $refs that another document's content closes, with external references on:
// one wholly in that document (GitHub #547), one closing through it and back
// (GitHub #768), and one a source loop that two $refs enter through it (GitHub
// #771). None overflows the stack, and each is reported once, at its least
// member, in the document holding it, in either declaration order. Its message
// names a member of another document than that one by its name.
func TestCompile_ALoopThroughAnotherDocumentIsReportedOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		source, other []string
		site          ir.Provenance
		names         bool
	}{
		{"wholly in the other document",
			[]string{"    R: {$ref: './other.yaml#/components/schemas/X'}\n"},
			[]string{"    X: {$ref: '#/components/schemas/Y'}\n", "    Y: {$ref: '#/components/schemas/X'}\n"},
			ir.Provenance{Source: 1, Pointer: "/components/schemas/X"}, false},
		{"through the other document and back",
			[]string{"    P: {$ref: 'other.yaml#/components/schemas/A'}\n", "    W: {$ref: '#/components/schemas/P'}\n"},
			[]string{"    A: {$ref: 'root.yaml#/components/schemas/W'}\n"},
			ir.Provenance{Pointer: "/components/schemas/P"}, true},
		{"a source loop entered through the other document",
			[]string{"    E1: {$ref: 'other.yaml#/components/schemas/O'}\n", "    E2: {$ref: 'other.yaml#/components/schemas/O'}\n",
				"    L1: {$ref: '#/components/schemas/L2'}\n", "    L2: {$ref: 'root.yaml#/components/schemas/L1'}\n"},
			[]string{"    O: {$ref: './root.yaml#/components/schemas/L1'}\n"},
			ir.Provenance{Pointer: "/components/schemas/L1"}, false},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		compiled := make([][]string, 0, 2)
		for _, reversed := range []bool{false, true} {
			source, other := slices.Clone(tc.source), slices.Clone(tc.other)
			if reversed {
				slices.Reverse(source)
				slices.Reverse(other)
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"),
				[]byte("components:\n  schemas:\n"+strings.Join(other, "")), 0o600))
			root := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
				strings.Join(source, "")
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err, tc.name)
			require.NotNil(t, doc, "%s: the rest of the document lowers", tc.name)
			var sites []ir.Provenance
			var seen []string
			for _, d := range diags {
				if d.Code == diag.CyclicRef {
					sites = append(sites, d.Provenance)
					assert.Equal(t, tc.names, strings.Contains(d.Message, " in "+filepath.Join(dir, "other.yaml")),
						"%s: a member of the other document is named by its name: %s", tc.name, d.Message)
				}
				seen = append(seen, fmt.Sprintf("%s %d%s %s", d.Code, d.Provenance.Source, d.Provenance.Pointer, d.Message))
			}
			assert.Equal(t, []ir.Provenance{tc.site}, sites, "%s, reversed=%t: %+v", tc.name, reversed, diags)
			slices.Sort(seen)
			compiled = append(compiled, seen)
		}
		assert.Equal(t, compiled[0], compiled[1], "%s: declaration order decided a report", tc.name)
	}
}

// TestCompile_ReferenceObjectCycleIsReportedUnresolved pins what the compile
// does with a chain of reference objects that fails: it reaches the lowering,
// and the resolver's failure is reported unresolved at the reference, never as
// a cycle refused before parsing.
func TestCompile_ReferenceObjectCycleIsReportedUnresolved(t *testing.T) {
	t.Parallel()
	for _, tc := range referenceObjectCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			_, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: data}}, compilers.Options{})
			require.NoError(t, err, "a failing reference is a spec problem, not a Go error")
			for _, d := range diags {
				assert.NotEqual(t, diag.CyclicRef, d.Code, "the resolver reports this itself: %+v", d)
			}
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, tc.at),
				"the reference is reported unresolved at %s; got %+v", tc.at, diags)
		})
	}
}

// TestCompile_AnchorFragmentIsLeftToTheResolver is the compiler-level half of
// the GitHub #523 fix. '#x-s' names a $anchor, not a pointer, so it must not
// be read as the pointer '/x-s' and walked from the document root as a key,
// which would close a loop on x-s that no resolver enters. Milestone 1
// resolves no anchors, so the reference still does not resolve; it is reported
// unresolved where it is written, and no cycle is reported.
func TestCompile_AnchorFragmentIsLeftToTheResolver(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-s:
  $ref: '#x-s'
components:
  schemas:
    A:
      $ref: '#x-s'
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "anchor.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err, "an unresolved anchor reference is a spec problem, not a Go error")
	require.NotNil(t, doc, "the document is no longer refused as a cycle")
	for _, d := range diags {
		assert.NotEqual(t, diag.CyclicRef, d.Code,
			"'#x-s' names a $anchor, which the resolver's pointer walk never enters: %+v", d)
	}
	assertHasErrorCode(t, diags, diag.UnresolvedRef)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/A"),
		"the reference is reported unresolved where it is written; got %+v", diags)
}

// TestCompile_NonUTF8FragmentReachesNoIdentifier is the compiler-level half of
// the GitHub #520 fix. A fragment that decodes to bytes that are not UTF-8 must
// never reach a PropID, since no document key can spell them and the IR cannot
// encode one that does. Before the fix, the multipart body's encoding key was
// derived from the raw byte the fragment decoded to, and the document could not
// be marshalled at all.
func TestCompile_NonUTF8FragmentReachesNoIdentifier(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /upload:
    post:
      requestBody:
        content:
          multipart/form-data:
            schema:
              $ref: '#/components/schemas/%FF'
              properties:
                file: {type: string, format: binary}
            encoding:
              file: {contentType: application/octet-stream}
      responses:
        "204": {description: ok}
components:
  schemas:
    Form: {type: object}
`
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "multipart.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)

	_, err = json.Marshal(doc)
	require.NoError(t, err, "no byte that is not UTF-8 reaches the document")

	// The resolver's own error for this reference names the key it could not
	// find, and diag.Newf scrubs that byte to U+FFFD so the message stays
	// encodable. That is by design, so the check covers everything else.
	withoutDiagnostics := *doc
	withoutDiagnostics.Diagnostics = nil
	clean, err := json.Marshal(&withoutDiagnostics)
	require.NoError(t, err)
	assert.NotContains(t, string(clean), "\ufffd",
		"U+FFFD must not reach anything but a diagnostic message")

	op := openapitest.FirstOp(t, doc.Services[0])
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	want := ir.PropID("p/openapi" + ids.Ptr("paths", "/upload", "post", "requestBody",
		"content", "multipart/form-data", "schema", "properties", "file"))
	enc := op.Request.Contents[0].Encoding
	require.Len(t, enc, 1, "exactly one part is configured; got %v", enc)
	_, ok := enc[want]
	assert.True(t, ok, "the encoding key derives from the body's own local position; got %v", enc)
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

// TestDetectCycles_ComponentOnlyCyclesLeftToResolver pins that a cycle of
// reference objects held in components is no cycle to the alias scan, and is
// reported unresolved by the parser's resolver, where a loop of schema $refs
// is the compiler's own to report.
func TestDetectCycles_ComponentOnlyCyclesLeftToResolver(t *testing.T) {
	t.Parallel()
	for _, tc := range componentOnlyCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scan.Aliases(scan.InSource(0), scanIndex(t, tc.data), openapi.DefaultMaxAliasSurplus),
				"a components-only cycle is the resolver's to report")

			_, diags, err := openapi.New().Compile(t.Context(),
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
	// legally (no degenerate cycle), so neither the alias scan nor the binder may
	// start reporting valid documents it previously accepted.
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

// TestCompile_RefShapedDataNotRefused pins that a structure shaped like a $ref
// outside a schema position, or a loop-free use of aliases, compiles and is not
// reported as a cycle. A cycle is reported and refuses nothing, so a $ref read
// where no resolver reads one shows as that diagnostic.
func TestCompile_RefShapedDataNotRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
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
// empty token changes a verdict. Reading '/A/' as stopping at A made this shape
// a cycle. The token names the key "" of A, which A does not hold, so the
// reference is reported unresolved where it is written and no cycle is.
func TestCompile_SchemaEmptyPointerSegmentIsUnresolved(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/A/'}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "schema_empty_segment.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	assert.NotNil(t, doc)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/A"),
		"the reference is reported unresolved where it is written; got %+v", diags)
	assert.Empty(t, cyclicRefSites(diags), "a pointer that names an undeclared key is unresolved, not cyclic")
}

// reproducerDir is where the cycle fixtures live. The reader below, the table
// guard above and the fuzz seeds all derive their paths from it, so none of them
// can end up looking somewhere the others are not — which matters most for the
// seed loader, since it ignores a read error and a wrong path there would cost
// the fuzzer its seeds in silence.
const reproducerDir = "../../../testdata/openapi"

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reproducerDir, file+".yaml"))
	require.NoError(t, err)
	return data
}

// TestCompile_MergeChainPastBoundStillCompiles pins that a legal document whose
// `<<` merge chain runs past the mapping view's depth bound is compiled. The
// alias scan weighs aliases rather than expanding merges through that view, so
// the chain leaves its protection complete, and no diagnostic refuses the
// source or reports an incomplete scan.
func TestCompile_MergeChainPastBoundStillCompiles(t *testing.T) {
	t.Parallel()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "deep-merge.yaml", Data: []byte(ynode.MergeChainSpec(200))}},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "a legal document is still compiled")
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "no diagnostic refuses the source: %+v", d)
		assert.NotEqual(t, diag.CycleScanFailed, d.Code, "the alias scan completes: %+v", d)
	}
}

// FuzzCycleDetector compiles arbitrary bytes seeded from the cycle corpus.
// Whatever a source holds, the compile must not panic, overflow the stack or
// hang, and must come back with a document or with a refusal: an error
// diagnostic saying why no document came back, or a Go error alone, which an
// empty source raises. A cycle of schema $refs refuses nothing, so a corpus
// seed comes back with a document.
func FuzzCycleDetector(f *testing.F) {
	for _, file := range cycleFixtureFiles() {
		if data, err := os.ReadFile(filepath.Join(reproducerDir, file+".yaml")); err == nil {
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
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
		if err != nil {
			assert.Nil(t, doc, "a Go error returns no document")
			return
		}
		if doc == nil {
			assert.True(t, ir.HasError(diags), "no document came back, and no error says why: %+v", diags)
		}
	})
}

// TestCompile_ReentrantPrefixReportsAlikeInBothDeclarationOrders pins that a
// pointer through a reference already on its chain is reported the same way
// whichever reference is declared first: /b's pointer passes through /a, whose
// own $ref leads to /b. Each is reported unresolved where it is written, and
// neither is refused as a cycle.
func TestCompile_ReentrantPrefixReportsAlikeInBothDeclarationOrders(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n"
	orders := []struct{ name, body string }{
		{"dangling-declared-first", "  /b: {$ref: '#/paths/~1a/t'}\n  /a: {$ref: '#/paths/~1b'}\n"},
		{"dangling-declared-last", "  /a: {$ref: '#/paths/~1b'}\n  /b: {$ref: '#/paths/~1a/t'}\n"},
	}
	for _, tc := range orders {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "order.yaml", Data: []byte(head + tc.body)}},
				compilers.Options{})
			require.NoError(t, err, "a failing reference is a spec problem, not a Go error")
			var sites []string
			for _, d := range diags {
				require.Equal(t, diag.UnresolvedRef, d.Code, "%+v", d)
				sites = append(sites, string(d.Provenance.Pointer))
			}
			assert.ElementsMatch(t, []string{"/paths/~1a", "/paths/~1b"}, sites)
		})
	}
}
