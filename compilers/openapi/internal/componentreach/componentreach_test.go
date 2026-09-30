package componentreach_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/componentreach"
)

// sections is the retained set every case below uses: the sections a compiler
// lowers only where a reference finds them.
var sections = []string{"responses", "parameters", "examples", "requestBodies",
	"headers", "links", "callbacks", "pathItems", "mediaTypes"}

// parse returns the root node of a YAML document.
func parse(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1)
	return doc.Content[0]
}

// pointers returns the pointers of the entries found, as strings.
func pointers(entries []componentreach.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, string(e.Pointer))
	}
	return out
}

// TestUnreferenced_DeclaresNothingToRetain covers the two short circuits: a
// document with no components object at all, and a caller retaining no section.
func TestUnreferenced_DeclaresNothingToRetain(t *testing.T) {
	t.Parallel()
	assert.Empty(t, componentreach.Unreferenced(nil, sections), "no tree, nothing to walk")
	assert.Empty(t, componentreach.Unreferenced(parse(t, "paths: {}\n"), sections),
		"a document with no components object declares no entry")
	assert.Empty(t, componentreach.Unreferenced(parse(t, "components: {schemas: {A: {type: string}}}\n"), nil),
		"a caller retaining no section asks for nothing")
}

// TestUnreferenced_EverySectionKeptWhenNothingReferencesIt is the case the fix
// exists for: one entry in each retained section, no `$ref` anywhere, so every
// one of them is kept — in the order the document declares them.
func TestUnreferenced_EverySectionKeptWhenNothingReferencesIt(t *testing.T) {
	t.Parallel()
	root := parse(t, `components:
  responses: {R: {description: ok}}
  parameters: {P: {name: p, in: query, schema: {type: string}}}
  examples: {E: {value: 1}}
  requestBodies: {B: {content: {application/json: {schema: {type: string}}}}}
  headers: {H: {schema: {type: string}}}
  links: {L: {operationId: op}}
  callbacks: {C: {'{$request.body#/url}': {post: {responses: {200: {description: ok}}}}}}
  pathItems: {I: {get: {responses: {200: {description: ok}}}}}
  mediaTypes: {M: {schema: {type: string}}}
  schemas: {S: {type: string}}
  securitySchemes: {K: {type: apiKey, in: header, name: X-K}}
`)
	entries := componentreach.Unreferenced(root, sections)
	assert.Equal(t, []string{
		"/components/responses/R",
		"/components/parameters/P",
		"/components/examples/E",
		"/components/requestBodies/B",
		"/components/headers/H",
		"/components/links/L",
		"/components/callbacks/C",
		"/components/pathItems/I",
		"/components/mediaTypes/M",
	}, pointers(entries), "every retained section's every entry, in document order")
	for _, e := range entries {
		assert.NotNil(t, e.Node, "each entry carries its raw node")
	}
}

// TestUnreferenced_ReferenceFromThePathsKeepsTheEntry pins the referenced side:
// an entry a `$ref` outside the retained sections names is not returned, and a
// second entry in the same section that nothing names still is.
func TestUnreferenced_ReferenceFromThePathsKeepsTheEntry(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/Used'}
        "404": {$ref: '#/components/responses/Gone'}
components:
  responses:
    Used:
      description: ok
      headers: {X: {$ref: '#/components/headers/H', schema: {type: string}}}
    Gone: {description: no}
  parameters: {Unused: {name: u, in: query, schema: {type: string}}}
`)
	assert.Equal(t, []string{"/components/parameters/Unused"}, pointers(componentreach.Unreferenced(root, sections)),
		"the response the paths name is reached, the one only the other mount names is not, and the parameter is untouched")
}

// TestUnreferenced_SchemasAreRootsNotSubjects pins that the two sections that
// lower unconditionally are walked as roots: a `$ref` written inside a component
// schema makes its target referenced.
func TestUnreferenced_SchemasAreRootsNotSubjects(t *testing.T) {
	t.Parallel()
	root := parse(t, `components:
  schemas:
    S: {type: object, properties: {x: {$ref: '#/components/parameters/P'}}}
  parameters: {P: {name: p, in: query, schema: {type: string}}}
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections),
		"a reference inside components/schemas counts, and securitySchemes is walked the same way")
}

// TestUnreferenced_ReachableEntryExpandsItsOwnReferences is the transitive rule's
// witness: an entry the paths name is reached, and the entry *it* names is
// reached in turn.
func TestUnreferenced_ReachableEntryExpandsItsOwnReferences(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/R'}
components:
  responses:
    R:
      description: ok
      content:
        application/json: {schema: {$ref: '#/components/parameters/P'}}
  parameters: {P: {name: p, in: query, schema: {type: string}}}
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections),
		"the reached response's own reference reaches the parameter")
}

// TestUnreferenced_UnreachableEntryDoesNotExpandItsOwnReferences is the other
// half, and the reason the rule is transitive rather than syntactic: a `$ref`
// inside an entry nothing reaches does not make its target referenced, so the
// target is kept too rather than being lowered on behalf of an entry that
// vanishes.
func TestUnreferenced_UnreachableEntryDoesNotExpandItsOwnReferences(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths: {}
components:
  responses:
    R:
      description: ok
      content:
        application/json: {schema: {$ref: '#/components/parameters/P'}}
  parameters: {P: {name: p, in: query, schema: {type: string}}}
`)
	assert.Equal(t, []string{"/components/responses/R", "/components/parameters/P"},
		pointers(componentreach.Unreferenced(root, sections)))
}

// TestUnreferenced_CyclicEntriesBothKeptTerminates pins the bound the reach walk
// does not need but must survive: two entries that name each other and that
// nothing outside names are both kept, and the walk terminates.
func TestUnreferenced_CyclicEntriesBothKeptTerminates(t *testing.T) {
	t.Parallel()
	root := parse(t, `components:
  responses:
    A: {description: a, headers: {X: {$ref: '#/components/parameters/B', schema: {type: string}}}}
  parameters:
    B: {name: b, in: query, schema: {type: string}, examples: {e: {$ref: '#/components/responses/A'}}}
`)
	assert.Equal(t, []string{"/components/responses/A", "/components/parameters/B"},
		pointers(componentreach.Unreferenced(root, sections)))
}

// TestUnreferenced_CyclicEntriesBothReachedTerminates is the same cycle with a
// root: the paths name one of them, so both are reached and neither is kept.
func TestUnreferenced_CyclicEntriesBothReachedTerminates(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/A'}
components:
  responses:
    A: {description: a, headers: {X: {$ref: '#/components/parameters/B', schema: {type: string}}}}
  parameters:
    B: {name: b, in: query, schema: {type: string}, examples: {e: {$ref: '#/components/responses/A'}}}
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections))
}

// TestUnreferenced_ExternalAndNonScalarReferencesAreNoRoots pins what a
// reference that names nothing here does: another document reaches no local
// entry, and a `$ref` whose value is not a scalar is not a reference at all.
func TestUnreferenced_ExternalAndNonScalarReferencesAreNoRoots(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      responses:
        "200": {$ref: 'other.yaml#/components/responses/R'}
        "404": {$ref: {nested: '#/components/responses/R'}}
        "500": {$ref: 'no-fragment'}
components:
  responses: {R: {description: ok}}
`)
	assert.Equal(t, []string{"/components/responses/R"},
		pointers(componentreach.Unreferenced(root, sections)),
		"no reference here names a local entry")
}

// TestUnreferenced_ReferenceThroughAnAliasCounts pins that the walk follows a
// YAML alias, which is the one shape where the same node is written once and
// read from another position.
func TestUnreferenced_ReferenceThroughAnAliasCounts(t *testing.T) {
	t.Parallel()
	root := parse(t, `components:
  responses: {R: {description: ok}}
paths:
  /a:
    get:
      x-shared: &shared {$ref: '#/components/responses/R'}
      responses:
        "200": *shared
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections))
}

// TestUnreferenced_NilNodeInTheTreeIsWalkedPast pins the walk's tolerance of a
// node no parser produces: an entry whose value is a sequence holding a nil is
// still declared, and neither walk dereferences the nil.
func TestUnreferenced_NilNodeInTheTreeIsWalkedPast(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("components"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("responses"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("R"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{nil}},
			}},
		}},
	}}
	assert.Equal(t, []string{"/components/responses/R"},
		pointers(componentreach.Unreferenced(root, sections)))
}

// TestUnreferenced_AliasChainPastTheBoundWalksNothing pins deref's bound: a
// chain long enough to exhaust it yields no node, so the walk skips the position
// rather than following it forever.
func TestUnreferenced_AliasChainPastTheBoundWalksNothing(t *testing.T) {
	t.Parallel()
	leaf := scalar("components")
	for range 40 {
		leaf = &yaml.Node{Kind: yaml.AliasNode, Alias: leaf}
	}
	// The alias sits where a mapping value would, so the walk dereferences it and
	// finds nothing rather than a components object.
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("x"), leaf}}
	assert.Empty(t, componentreach.Unreferenced(root, sections))
}

// scalar returns a plain scalar node.
func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// TestUnreferenced_OrderIsTheDocuments is the determinism rule: the entries come
// back in the order the document declares them, whatever that order is.
func TestUnreferenced_OrderIsTheDocuments(t *testing.T) {
	t.Parallel()
	spec := func(first, second string) *yaml.Node {
		return parse(t, "components:\n  responses:\n    "+first+": {description: a}\n    "+second+": {description: b}\n")
	}
	assert.Equal(t, []string{"/components/responses/A", "/components/responses/B"},
		pointers(componentreach.Unreferenced(spec("A", "B"), sections)))
	assert.Equal(t, []string{"/components/responses/B", "/components/responses/A"},
		pointers(componentreach.Unreferenced(spec("B", "A"), sections)))
}

// TestUnreferenced_PointerEscapesANameKeepsTheRefAddressable pins the one thing
// the pointer is used for: it has to equal the pointer a `$ref` writes, so a
// name holding a "/" is escaped the RFC 6901 way and the reference resolves.
func TestUnreferenced_PointerEscapesANameKeepsTheRefAddressable(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/a~1b'}
components:
  responses:
    a/b: {description: ok}
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections),
		"the escaped pointer the reference writes names the entry")
}

// TestUnreferenced_ComponentsThatIsNotAMappingDeclaresNothing pins the shape
// guard: a components value the walk cannot read as an object yields no entry
// rather than a panic.
func TestUnreferenced_ComponentsThatIsNotAMappingDeclaresNothing(t *testing.T) {
	t.Parallel()
	assert.Empty(t, componentreach.Unreferenced(parse(t, "components: hello\n"), sections))
}

// TestUnreferenced_SectionThatIsNotAMappingDeclaresNothing is the same guard one
// level down: a retained section written as a scalar declares no entries.
func TestUnreferenced_SectionThatIsNotAMappingDeclaresNothing(t *testing.T) {
	t.Parallel()
	assert.Empty(t, componentreach.Unreferenced(parse(t, "components: {responses: hello}\n"), sections),
		"a scalar has no entries, and the walk reads none")
}

// TestUnreferenced_HandleIsUnused keeps the exported Entry's fields honest: both
// are populated, and the pointer is a well-formed JSON pointer.
func TestUnreferenced_EntryPointerIsAJSONPointer(t *testing.T) {
	t.Parallel()
	entries := componentreach.Unreferenced(parse(t, "components: {responses: {R: {description: ok}}}\n"), sections)
	require.Len(t, entries, 1)
	assert.Equal(t, jsontext.Pointer("/components/responses/R"), entries[0].Pointer)
	assert.True(t, entries[0].Pointer.IsValid())
	assert.Equal(t, "R", entries[0].Pointer.LastToken())
}

// TestUnreferenced_ReferencesInsideASequenceCount pins the walk through a
// sequence: a `$ref` written in one is read like any other, which is what a
// `parameters: [$ref]` list on a path item is.
func TestUnreferenced_ReferencesInsideASequenceCount(t *testing.T) {
	t.Parallel()
	root := parse(t, `paths:
  /a:
    get:
      parameters:
        - {$ref: '#/components/parameters/P'}
components:
  parameters: {P: {name: p, in: query, schema: {type: string}}}
`)
	assert.Empty(t, componentreach.Unreferenced(root, sections))
}

// TestUnreferenced_ReachedEntryWalksPastANilNode pins the two tolerances a
// hand-built tree needs: a reached entry whose subtree holds a nil is still
// expanded, and neither walk dereferences the nil.
func TestUnreferenced_ReachedEntryWalksPastANilNode(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("paths"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("/a"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("get"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
					scalar("responses"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
						scalar("200"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
							scalar("$ref"), scalar("#/components/responses/R"),
						}},
					}},
				}},
			}},
		}},
		scalar("components"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("responses"), {Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("R"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{nil}},
			}},
		}},
	}}
	assert.Empty(t, componentreach.Unreferenced(root, sections),
		"the entry is reached, so it is not kept, and walking its subtree survived the nil")
}
