package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func assertTagsGrouping(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "pets", doc.Services[0].Groups[0].Name.Source)
	require.Len(t, doc.TagDefs, 1)
	assert.Equal(t, "pets", doc.TagDefs[0].Name)
}

// assertGroupIdentity pins what identifies a group: the rule that formed it and
// the key that rule read, never its name. Two pairs among the seven render from
// the same words — the declared tag "default" and the group untagged operations
// fall into, the tag "webhooks" and the webhook group — and two tags differ only
// by what RFC 6901 escapes. Each keeps an ID of its own. The IDs are written out
// rather than derived, so the test cannot agree with a change to the derivation
// (GitHub #673).
func assertGroupIdentity(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	got := map[ir.GroupID][]string{}
	for _, g := range doc.Services[0].Groups {
		require.NotContains(t, got, g.ID, "two groups share an ID")
		for _, op := range g.Operations {
			got[g.ID] = append(got[g.ID], op.Name.Source)
		}
	}
	assert.Equal(t, map[ir.GroupID][]string{
		"g/tags/default":  {"declared"},
		"g/default":       {"untagged"},
		"g/tags/webhooks": {"namedLikeWebhooks"},
		"g/tags/ghost":    {"undeclared"},
		"g/tags/a~1b":     {"slash"},
		"g/tags/a~01b":    {"tilde"},
		"g/webhooks":      {"onEvent"},
	}, got)
}

// assertParamIdentity pins what identifies a parameter: the operation it is
// mounted under, its name as one escaped token, and its location. Two
// parameters named id in one operation get two IDs; a path-item parameter is one
// parameter per operation it is copied into; a path item reached from two paths
// roots each mount's parameters at that mount. The IDs are written out rather
// than derived, so the test cannot agree with a change to the derivation, and
// every binding must name its own parameter's ID.
func assertParamIdentity(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	got := map[ir.OpID][]ir.ParamID{}
	svc := doc.Services[0]
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			for _, p := range op.Params {
				got[op.ID] = append(got[op.ID], p.ID)
			}
			require.Len(t, op.Bindings.HTTP, 1)
			var bound []ir.ParamID
			for _, pb := range op.Bindings.HTTP[0].ParamBindings {
				bound = append(bound, pb.Param)
			}
			assert.ElementsMatch(t, got[op.ID], bound, "%s: each binding names its own parameter", op.ID)
		}
	}
	assert.Equal(t, map[ir.OpID][]ir.ParamID{
		"op/openapi/paths/~1items~1{id}/get": {
			"param/openapi/paths/~1items~1{id}/get/parameters/id/query",
			"param/openapi/paths/~1items~1{id}/get/parameters/a~1b/query",
			"param/openapi/paths/~1items~1{id}/get/parameters/id/path",
		},
		"op/openapi/paths/~1items~1{id}/post": {
			"param/openapi/paths/~1items~1{id}/post/parameters/id/path",
		},
		"op/openapi/paths/~1a/get": {"param/openapi/paths/~1a/get/parameters/limit/query"},
		"op/openapi/paths/~1b/get": {"param/openapi/paths/~1b/get/parameters/limit/query"},
	}, got)
}

// assertEnumMemberIdentity pins what identifies an enum member: the value it
// holds, tagged by kind, under the enum that declares it. The list repeats a
// value, includes names that spell one canonical word sequence, and carries the
// characters a pointer token escapes, so a derivation from the name, the
// position or the unescaped value changes an ID. The IDs are written out, not
// derived. The repeat is kept, its ID ends in its occurrence number, and it is
// reported once at its own entry; the string spelling that ID's tail is no
// repeat.
func assertEnumMemberIdentity(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	memberIDs := func(id ir.TypeID) []ir.EnumMemberID {
		enum, ok := doc.Types[id].(*ir.Enum)
		require.True(t, ok, "%s is an enum", id)
		got := make([]ir.EnumMemberID, 0, len(enum.Members))
		for _, m := range enum.Members {
			got = append(got, m.ID)
		}
		return got
	}
	assert.Equal(t, []ir.EnumMemberID{
		"e/openapi/components/schemas/E/s:a-b",
		"e/openapi/components/schemas/E/s:a_b",
		"e/openapi/components/schemas/E/s:A B",
		"e/openapi/components/schemas/E/s:a~0b",
		"e/openapi/components/schemas/E/s:a-b/2",
		"e/openapi/components/schemas/E/s:a-b~12",
	}, memberIDs("t/openapi/components/schemas/E"))
	assert.Equal(t, []ir.EnumMemberID{
		"e/openapi/components/schemas/N/n:-1e0",
		"e/openapi/components/schemas/N/n:1e0",
		"e/openapi/components/schemas/N/n:0",
	}, memberIDs("t/openapi/components/schemas/N"))
	assert.Equal(t, []ir.EnumMemberID{
		"e/anon/paths/~1status/get/responses/200/content/application~1json/schema/s:open",
		"e/anon/paths/~1status/get/responses/200/content/application~1json/schema/s:closed",
	}, memberIDs("t/anon/paths/~1status/get/responses/200/content/application~1json/schema"))

	var repeats []string
	for _, d := range diags {
		if d.Code == "openapi/duplicate-enum-value" {
			repeats = append(repeats, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/schemas/E/enum/4"}, repeats)
}

func assertHTTPBinding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	hb := op.Bindings.HTTP[0]
	assert.Equal(t, "GET", hb.Method)
	assert.Equal(t, "/items/{id}", hb.URITemplate)
}

// assertTagsGrouping32 is the 3.2 counterpart of assertTagsGrouping, and the
// first case in the corpus to witness OperationGroup.Groups: a tag declaring a
// parent nests its group under that parent's, an operation's non-navigational
// tag does not become the group it is filed under, and the declared 3.2 tag
// metadata reaches the registry verbatim.
func assertTagsGrouping32(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.TagDefs, 4)
	require.Equal(t, "books", doc.TagDefs[0].Name, "TagDefs keep the document's declaration order")
	assert.Equal(t, "catalog", doc.TagDefs[0].Parent)
	assert.Equal(t, "nav", doc.TagDefs[0].Kind)
	assert.Equal(t, "nav", doc.TagDefs[1].Kind)
	assert.Equal(t, "badge", doc.TagDefs[2].Kind, "the non-navigational kind is recorded, not interpreted")
	assert.Empty(t, doc.TagDefs[2].Parent)

	require.Len(t, doc.Services, 1)
	svc := doc.Services[0]
	require.Len(t, svc.Groups, 1, "the child nests rather than becoming a second top-level group")
	catalog := svc.Groups[0]
	assert.Equal(t, "catalog", catalog.Name.Source)
	assert.Equal(t, "Everything the library holds", catalog.Docs.Description,
		"an ancestor created first on its child's behalf still carries its declared docs")
	require.Len(t, catalog.Operations, 1)
	assert.Equal(t, "showCatalog", catalog.Operations[0].Name.Source)

	require.Len(t, catalog.Groups, 2, "both children nest, in the order operations first reach them")
	assert.Equal(t, "magazines", catalog.Groups[1].Name.Source)
	books := catalog.Groups[0]
	assert.Equal(t, "books", books.Name.Source)
	assert.Equal(t, "Book operations", books.Docs.Description)
	require.Len(t, books.Operations, 1)
	listBooks := books.Operations[0]
	assert.Equal(t, "listBooks", listBooks.Name.Source)
	assert.Equal(t, []string{"beta", "books"}, listBooks.Tags,
		"tag membership keeps every tag the operation named, badge included")
}
