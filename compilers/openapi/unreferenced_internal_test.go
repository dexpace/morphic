package openapi

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestUnreferencedComponents_RetentionIsOrderIndependent is the hand-rolled
// two-order diff for GitHub #616. Document.Unmodeled is neither the type registry
// nor a diagnostic, so the corpus's order-invariance oracle does not see it: the
// same entries declared before and after the paths that reference one of them
// must be kept in the same set, located at the same pointers.
func TestUnreferencedComponents_RetentionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	const paths = `paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/Used'}
`
	const components = `components:
  responses:
    Used: {description: reached}
    Unused: {description: not reached}
`
	first, _ := parseFull(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"+components+paths)
	second, _ := parseFull(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\n"+paths+components)
	assert.Empty(t, cmp.Diff(first.Unmodeled, second.Unmodeled),
		"what is kept must not depend on which of the two the document writes first")
	require.Contains(t, first.Unmodeled, "openapi:components/responses/Unused")
}

// TestUnreferencedComponents_ChainKeepsBothEnds pins that an entry nothing
// lowers carries its own references with it: they are never followed, so the
// entry each names is kept too rather than vanishing one hop later.
func TestUnreferencedComponents_ChainKeepsBothEnds(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    R:
      description: nothing reaches this
      headers: {X: {$ref: '#/components/headers/H'}}
  headers:
    H: {schema: {type: string}}
`)
	require.Contains(t, doc.Unmodeled, "openapi:components/responses/R")
	require.Contains(t, doc.Unmodeled, "openapi:components/headers/H",
		"the target of an unlowered entry's own reference is kept too")
	assert.Equal(t, ir.ReasonNoIRHome, doc.Unmodeled["openapi:components/headers/H"].Reason)
}

// TestUnreferencedComponents_LoweredEntryCarriesItsOwnReferences is the same
// shape with the response lowered: lowering it follows its header's reference,
// so neither entry is kept beside what the IR already holds.
func TestUnreferencedComponents_LoweredEntryCarriesItsOwnReferences(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/R'}
components:
  responses:
    R:
      description: reached
      headers: {X: {$ref: '#/components/headers/H'}}
  headers:
    H: {schema: {type: string}}
`)
	assert.NotContains(t, doc.Unmodeled, "openapi:components/responses/R")
	assert.NotContains(t, doc.Unmodeled, "openapi:components/headers/H")
}

// TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget pins what
// "referenced" means: a $ref a lowering follows, not a $ref string written
// somewhere. Each document below names its entry only where no lowering
// follows a reference, so the entry's content reaches the IR nowhere else and
// is kept.
func TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, paths, components, kept string
	}{
		{"a response's links map", `
      responses:
        "200":
          description: ok
          links: {next: {$ref: '#/components/links/Next'}}`,
			"  links:\n    Next: {operationId: a}\n", "openapi:components/links/Next"},
		{"an extension", `
      x-meta: {$ref: '#/components/parameters/P'}
      responses: {"200": {description: ok}}`,
			"  parameters:\n    P: {name: p, in: query, schema: {type: string}}\n", "openapi:components/parameters/P"},
		{"an example's value", `
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
              example: {$ref: '#/components/examples/E'}`,
			"  examples:\n    E: {value: 1}\n", "openapi:components/examples/E"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a`+tc.paths+`
components:
`+tc.components)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Contains(t, doc.Unmodeled, tc.kept)
		})
	}
}

// TestUnreferencedComponents_ReadAsTheParserReadsThem holds how a section's
// entries are read to the parser's own reading of the same document: the names
// its model gives a section, which come through `<<` merge keys under their own
// name with no entry named `<<`, and include the empty one; and the entry a
// $ref resolves to, however it spells the name. Every entry the parser reads
// that no resolved reference reaches is kept. The literals pin today's parser,
// so a release that reads otherwise fails here even while the compiler still
// agrees with it.
func TestUnreferencedComponents_ReadAsTheParserReadsThem(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/components/parameters/%41bc'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    Abc: {name: abc, in: query, schema: {type: string}}
  responses:
    <<: {Merged: {description: merged in}}
    "": {description: nameless}
`
	parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	require.NotNil(t, parsed.Components, "the parser reads the components")
	responses := slices.Collect(parsed.Components.Responses.Keys())
	parameters := slices.Collect(parsed.Components.Parameters.Keys())
	reached := parserReached(t, parsed)
	assert.Equal(t, []string{"Merged", ""}, responses, "the parser's names for the responses section")
	assert.Equal(t, []string{"Abc"}, reached, "the parameters the parser resolves the reference to")

	doc, _ := parseFull(t, src)
	assert.Equal(t, slices.Sorted(slices.Values(responses)), keptUnder(doc, "responses"),
		"every response the parser reads, none reached, is kept under the name the parser gives it")
	assert.Equal(t, slices.DeleteFunc(slices.Clone(parameters), func(name string) bool {
		return slices.Contains(reached, name)
	}), keptUnder(doc, "parameters"), "a parameter the parser resolves a reference to is lowered, not kept")
}

// parserReached returns the names of the parameters entries the parser resolves
// the operation parameters of /a to, by the identity of the object it finds.
func parserReached(t *testing.T, parsed *soa.OpenAPI) []string {
	t.Helper()
	var reached []string
	for _, ref := range parsed.Paths.GetOrZero("/a").GetObject().Get().Parameters {
		_, err := ref.Resolve(t.Context(), soa.ResolveOptions{RootDocument: parsed,
			TargetLocation: "spec.yaml", DisableExternalRefs: true})
		require.NoError(t, err, "the parser resolves the reference")
		target := ref.GetReferenceResolutionInfo().Object.GetObject()
		for name, entry := range parsed.Components.Parameters.All() {
			if entry.GetObject() == target {
				reached = append(reached, name)
			}
		}
	}
	return reached
}

// keptUnder returns the sorted names of the entries of section that doc keeps
// verbatim as unreferenced components.
func keptUnder(doc *ir.Document, section string) []string {
	prefix := "openapi:components/" + section + "/"
	names := []string{}
	for key := range doc.Unmodeled {
		if name, ok := strings.CutPrefix(key, prefix); ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// TestUnreferencedComponents_SelfReferenceByFileNameReaches pins a reference
// naming this document by its file: the resolver reads it as internal, so the
// entry it names is lowered at the use and not also kept verbatim beside it.
func TestUnreferencedComponents_SelfReferenceByFileNameReaches(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	src := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: 'spec.yaml#/components/parameters/P'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(src)}},
		compilers.Options{FormatOptions: Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, openapitest.FindOp(t, doc, "a").Params, 1, "the reference resolves and the parameter lowers")
	assert.NotContains(t, doc.Unmodeled, "openapi:components/parameters/P")
}

// TestUnreferencedComponents_MergeDepthBoundIsReported pins the one way an
// entry can go unkept: a section merging through `<<` deeper than nodeview reads.
// The entries past the bound are not seen, so the retention says so rather than
// keeping a partial set in silence.
func TestUnreferencedComponents_MergeDepthBoundIsReported(t *testing.T) {
	t.Parallel()
	section := "{P: {name: p, in: query, schema: {type: string}}}"
	for range nodeview.MergeDepthLimit + 1 {
		section = "{<<: " + section + "}"
	}
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"+
		"components:\n  parameters: "+section+"\n")
	assert.NotContains(t, doc.Unmodeled, "openapi:components/parameters/P")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components"), "got %v", diags)
}

// TestComponentMediaTypes_KeptWholeAtEveryVersion pins the components/mediaTypes
// section while this compiler resolves nothing into it (GitHub #856): the map is
// kept whole with an info rather than called undefined, merged in through `<<`
// or not. Below 3.2, which does not define it, it is kept the same, and load
// adds that the field is a later version's (GitHub #864).
func TestComponentMediaTypes_KeptWholeAtEveryVersion(t *testing.T) {
	t.Parallel()
	const body = `paths: {}
components:
  mediaTypes:
    Event: {schema: {type: string}}
`
	const merged = `paths: {}
components:
  <<: {mediaTypes: {Event: {schema: {type: string}}}}
`
	for _, tc := range []struct {
		name, version, body string
	}{
		{"3.2", "3.2.0", body},
		{"3.2 merged in", "3.2.0", merged},
		{"3.1", "3.1.0", body},
		{"3.1 merged in", "3.1.0", merged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, "openapi: "+tc.version+"\ninfo: {title: T, version: \"1\"}\n"+tc.body)
			kept, ok := doc.Unmodeled["openapi:components/mediaTypes"]
			require.True(t, ok, "the section is kept whole")
			assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
			assert.NotEmpty(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
				"/components/mediaTypes"))
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			assert.Equal(t, tc.version != "3.2.0",
				openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, "/components/mediaTypes"),
				"a field merged in through `<<` is written as the parser reads it")
		})
	}
}
