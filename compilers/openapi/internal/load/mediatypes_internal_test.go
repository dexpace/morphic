// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/engine/reader"
)

// mediaTypeSpec declares components/mediaTypes entries of every shape the
// section reads, and content entries reaching them, one through a YAML alias.
const mediaTypeSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: &use {$ref: '#/components/mediaTypes/Pet'}
            text/plain: {schema: {type: string}}
            text/csv: {$ref: null}
  /b:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: *use
components:
  mediaTypes:
    Pet: {schema: {type: string}}
    Alias: {$ref: '#/components/mediaTypes/Pet'}
    Scalar: 5
    Null: {$ref: null}
    <<: {Merged: {schema: {type: integer}}}
`

// contentOf returns the media type the response 200 of path's get holds
// under mt.
func contentOf(t *testing.T, doc *soa.OpenAPI, path, mt string) *soa.MediaType {
	t.Helper()
	item, ok := doc.GetPaths().Get(path)
	require.True(t, ok)
	media, ok := response200(t, doc, path).GetObject().GetContent().Get(mt)
	require.True(t, ok, "%s %s", item, mt)
	return media
}

// faulting is a mediaTypeBuild that faults on every node.
func faulting(context.Context, *yaml.Node) (*refscope.ReferencedMediaType, error) {
	return nil, errors.New("planted fault")
}

// TestModelMediaTypes_ModelsEachDeclaredEntry pins which components/mediaTypes
// entries are modelled, at which pointer, read as the parser reads the
// section: a mapping and an alias of one, one merged in through `<<`, and
// neither a scalar nor one whose $ref holds no value, which the rule table
// reports. Only an entry that is no reference is the model a resolution
// ending on it is handed.
func TestModelMediaTypes_ModelsEachDeclaredEntry(t *testing.T) {
	t.Parallel()
	m, faults := modelMediaTypes(t.Context(), modelOf(t, mediaTypeSpec), scan.InSource(0))
	require.Empty(t, faults)
	pointers := make([]jsontext.Pointer, 0, len(m.declared))
	for _, d := range m.declared {
		pointers = append(pointers, d.pointer)
	}
	assert.Equal(t, []jsontext.Pointer{"/components/mediaTypes/Merged", "/components/mediaTypes/Pet",
		"/components/mediaTypes/Alias"}, pointers, "a merged entry first, as the parser reads a mapping")
	assert.False(t, m.declared[1].model.IsReference())
	assert.True(t, m.declared[2].model.IsReference())
	assert.Len(t, m.canonical, 2, "the alias is no declaration a chain ends on")
}

// TestMediaTypesFrom_AParserFaultIsReported pins a declared entry the parser
// faults building: reported where it is written, and not modelled.
func TestMediaTypesFrom_AParserFaultIsReported(t *testing.T) {
	t.Parallel()
	m, faults := mediaTypesFrom(t.Context(), modelOf(t, mediaTypeSpec), scan.InSource(0), faulting)
	assert.Empty(t, m.declared)
	require.Len(t, faults, 3, "Pet, Alias and Merged; the scalar and the null $ref are the table's")
	assert.Equal(t, diag.Validation, faults[0].Code)
	assert.Contains(t, faults[0].Message, "planted fault")
}

// TestMediaTypes_UseAnswersByTheMappingWritten pins Use: a content entry
// writing no $ref is none, one the walk met has its model, both positions an
// alias mounts it at the same one, and one whose $ref holds no value has none.
func TestMediaTypes_UseAnswersByTheMappingWritten(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, mediaTypeSpec)
	m, _ := modelMediaTypes(t.Context(), doc, scan.InSource(0))
	for range walkSource(t.Context(), doc, Siblings{}, m) {
		continue
	}
	_, isUse := m.Use(contentOf(t, doc, "/a", "text/plain"))
	assert.False(t, isUse)
	a, isUse := m.Use(contentOf(t, doc, "/a", "application/json"))
	require.True(t, isUse)
	require.NotNil(t, a)
	b, _ := m.Use(contentOf(t, doc, "/b", "application/json"))
	assert.Same(t, a, b, "an alias mounts one mapping, modelled once")
	none, isUse := m.Use(contentOf(t, doc, "/a", "text/csv"))
	assert.True(t, isUse)
	assert.Nil(t, none)
	unmet, isUse := MediaTypes{}.Use(contentOf(t, doc, "/a", "application/json"))
	assert.True(t, isUse)
	assert.Nil(t, unmet, "a use no walk met has no model")
}

// TestWalkSource_YieldsEachMediaTypesModel pins what the source's walk yields
// beyond the library's: after each Media Type Object its model, at its
// location, and after the document each declared entry's walk at its pointer.
// The zero MediaTypes yields the library's walk alone.
func TestWalkSource_YieldsEachMediaTypesModel(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, mediaTypeSpec)
	m, _ := modelMediaTypes(t.Context(), doc, scan.InSource(0))
	var models []string
	for item := range walkSource(t.Context(), doc, Siblings{}, m) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			if r, ok := model.(*refscope.ReferencedMediaType); ok {
				kind := "inline"
				if r.IsReference() {
					kind = "ref"
				}
				models = append(models, item.Location.ToJSONPointer().String()+" "+kind)
			}
			return nil
		}})
	}
	assert.Equal(t, []string{
		"/paths/~1a/get/responses/200/content/application~1json ref",
		"/paths/~1a/get/responses/200/content/text~1plain inline",
		"/paths/~1b/get/responses/200/content/application~1json ref",
		"/components/mediaTypes/Merged inline",
		"/components/mediaTypes/Pet inline",
		"/components/mediaTypes/Alias ref",
	}, models, "each once")
	assert.Equal(t, walkSites(walk(t.Context(), doc)), walkSites(walkSource(t.Context(), doc, Siblings{}, MediaTypes{})))
}

// TestWalkSource_StopsWhereItsConsumerStops pins that each stage of the walk
// stops when its consumer does: after a library item, after a model, inside a
// declared entry's walk, and inside a model's own walk.
func TestWalkSource_StopsWhereItsConsumerStops(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, mediaTypeSpec)
	m, _ := modelMediaTypes(t.Context(), doc, scan.InSource(0))
	total := 0
	for range walkSource(t.Context(), doc, Siblings{}, m) {
		total++
	}
	for n := 1; n < total; n++ {
		seen := 0
		for range walkSource(t.Context(), doc, Siblings{}, m) {
			if seen++; seen == n {
				break
			}
		}
		assert.Equal(t, n, seen)
	}
	for range mediaTypeWalk(t.Context(), m.declared[1].model) {
		break
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Empty(t, walkSites(mediaTypeWalk(ctx, m.declared[1].model)), "a cancelled walk yields nothing")
	assert.Len(t, walkSites(mediaTypeWalk(t.Context(), m.declared[2].model)), 1, "a reference is walked alone")
}

// TestMediaTypes_MetSkipsWhatHoldsNoMapping pins met on a media type with no
// node, which a parsed document never yields, and on none: neither yields a
// model, and neither is held as canonical.
func TestMediaTypes_MetSkipsWhatHoldsNoMapping(t *testing.T) {
	t.Parallel()
	m, _ := modelMediaTypes(t.Context(), modelOf(t, mediaTypeSpec), scan.InSource(0))
	before := len(m.canonical)
	assert.Nil(t, m.met(t.Context(), nil))
	assert.Nil(t, m.met(t.Context(), &soa.MediaType{}))
	assert.Len(t, m.canonical, before)
	_, isUse := m.Use(&soa.MediaType{})
	assert.False(t, isUse, "a media type with no mapping writes no $ref")
}

// TestModelMediaTypes_StopsOnceCancelled pins that a cancelled context models no
// entry, for the phase refusing the next step to see.
func TestModelMediaTypes_StopsOnceCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m, faults := modelMediaTypes(ctx, modelOf(t, mediaTypeSpec), scan.InSource(0))
	assert.Empty(t, m.declared)
	assert.Empty(t, faults)
}

// TestModelItem_HandsTheModelToAnyAlone pins the walk item of a model the
// library's walk does not know: Any is handed the model and its error is
// returned, and a matcher without Any matches nothing.
func TestModelItem_HandsTheModelToAnyAlone(t *testing.T) {
	t.Parallel()
	model := &refscope.ReferencedMediaType{}
	item := modelItem(model, nil)
	assert.NoError(t, item.Match(soa.Matcher{}))
	planted := errors.New("planted")
	var got any
	assert.ErrorIs(t, item.Match(soa.Matcher{Any: func(m any) error { got = m; return planted }}), planted)
	assert.Same(t, model, got)
}

// TestResolve_AMediaTypeUseTheParserFaultsOnIsReportedOnce pins the fault of a
// content entry the parser cannot build a reference from although its $ref is
// a scalar: reported once, where the walk first met the mapping, though an
// alias mounts it at a second position, and in another document where one
// writes it.
func TestResolve_AMediaTypeUseTheParserFaultsOnIsReportedOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.2.0
info: {title: O, version: "1"}
paths: {}
components:
  responses:
    R:
      description: ok
      content:
        application/json: {$ref: '#/components/mediaTypes/X'}
`), 0o600))
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: &use {$ref: '#/components/mediaTypes/X'}
        "404": {$ref: 'other.yaml#/components/responses/R'}
  /b:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: *use
`
	doc := modelOf(t, spec)
	media, _ := mediaTypesFrom(t.Context(), doc, scan.InSource(0), faulting)
	_, _, _, pass, _, err := resolveExternal(t.Context(), doc, Siblings{}, media,
		sourceDocument{path: root}, Options{Reader: reader.Disk{}}, nil)
	require.NoError(t, err)
	faults := map[string][]string{}
	for doc, ds := range pass.failures {
		for _, d := range ds {
			if d.Code == diag.Validation {
				faults[filepath.Base(doc)] = append(faults[filepath.Base(doc)], string(d.Provenance.Pointer))
			}
		}
	}
	assert.Equal(t, map[string][]string{".": {"/paths/~1a/get/responses/200/content/application~1json"},
		"other.yaml": {"/components/responses/R/content/application~1json"}}, faults)
}

// TestInternalBase_IsWhatTheResolverReadsAnInternalRefAgainst pins the base a
// media type is held under: the path the source was read from, the $self it
// writes in its place, and none for a base the resolver cannot name, under
// which nothing is held.
func TestInternalBase_IsWhatTheResolverReadsAnInternalRefAgainst(t *testing.T) {
	t.Parallel()
	plain := modelOf(t, "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
	assert.Equal(t, "/specs/root.yaml", internalBase(plain, "/specs/root.yaml"))
	self := modelOf(t, "openapi: 3.2.0\n$self: https://example.com/api.yaml\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
	assert.Equal(t, "https://example.com/api.yaml", internalBase(self, "/specs/root.yaml"))
	assert.Empty(t, internalBase(plain, "http://[::1"))
	p := &resolution{}
	p.holdMediaType(plain, "/x", &refscope.ReferencedMediaType{})
	_, held := plain.GetCachedReferencedObject("#/x")
	assert.False(t, held)
}

// TestLoad_AMediaTypeRefEndsOnTheDeclarationsOwnModel pins that a content
// entry's chain ends on the model this phase holds of the declaration, whose
// references the walk resolved, however the resolver reached it: by the key
// load holds it under, against the source's path or the $self it writes, a
// content entry of the source included, and by a spelling that misses it, a
// pointer ending in '/' the resolver reads through raw YAML as the entry
// (adopt; docs/parser-library.md#trailing-empty-token).
func TestLoad_AMediaTypeRefEndsOnTheDeclarationsOwnModel(t *testing.T) {
	t.Parallel()
	const spec = `info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Pet'}
            text/plain: {$ref: '#/components/mediaTypes/Pet/'}
            text/csv: {$ref: '#/paths/~1b/get/responses/200/content/application~1problem+json'}
  /b:
    get:
      responses:
        "200":
          description: ok
          content:
            application/problem+json: {schema: {type: string}}
components:
  mediaTypes:
    Pet: {schema: {type: string}}
`
	for name, head := range map[string]string{"read from its path": "openapi: 3.2.0\n",
		"under its $self": "openapi: 3.2.0\n$self: https://example.com/api/spec.yaml\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(head + spec)},
				Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)
			require.Empty(t, diags)
			declared := doc.MediaTypes.declared[0].model
			for mt, want := range map[string]*soa.MediaType{"application/json": declared.GetObject(),
				"text/plain": declared.GetObject(), "text/csv": contentOf(t, doc.Doc, "/b", "application/problem+json")} {
				use, isUse := doc.MediaTypes.Use(contentOf(t, doc.Doc, "/a", mt))
				require.True(t, isUse)
				require.NotNil(t, use)
				assert.Same(t, want, use.GetObject(), mt)
			}
		})
	}
}

// TestLoad_AMediaTypeRefToNoObjectIsReportedAsOne pins a content entry whose
// $ref names a position holding no object: reported once, at the $ref, worded
// as a schema $ref to no schema is, naming the kind it asked for.
func TestLoad_AMediaTypeRefToNoObjectIsReportedAsOne(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/x-text'}
x-text: just text
`)}, Options{})
	require.NoError(t, err)
	require.Len(t, diags, 1, "got %v", diags)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
	assert.Contains(t, diags[0].Message, "holds no media type")
}

// TestMediaTypes_AdoptLeavesAnEndItHoldsNoModelOf pins adopt where it changes
// nothing: a chain that stopped, one ending outside the source, and one ending
// on a node no model was built from.
func TestMediaTypes_AdoptLeavesAnEndItHoldsNoModelOf(t *testing.T) {
	t.Parallel()
	m, _ := modelMediaTypes(t.Context(), modelOf(t, mediaTypeSpec), scan.InSource(0))
	var source any = &soa.OpenAPI{}
	var other any = &yaml.Node{}
	assert.False(t, m.adopt(chain{stopped: "#/x"}))
	assert.False(t, m.adopt(chain{records: []record{{document: &other, node: &yaml.Node{}}}}))
	assert.False(t, m.adopt(chain{records: []record{{document: &source, node: &yaml.Node{}}}}))
	assert.False(t, m.adopt(chain{records: []record{{document: &source}}}))
}

// TestBuildMediaType_RegistersItsModelOnce pins that building twice leaves the
// marshaller knowing the model's core type, whose factory it then builds an
// instance with, and builds alike.
func TestBuildMediaType_RegistersItsModelOnce(t *testing.T) {
	t.Parallel()
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`{$ref: '#/a', summary: s}`), &node))
	for range 2 {
		model, err := buildMediaType(t.Context(), node.Content[0])
		require.NoError(t, err)
		assert.Equal(t, "s", model.GetSummary())
	}
	coreType := reflect.TypeFor[core.Reference[*core.MediaType]]()
	require.True(t, marshaller.IsRegistered(coreType))
	assert.IsType(t, &core.Reference[*core.MediaType]{}, marshaller.CreateInstance(coreType).Interface())
}
