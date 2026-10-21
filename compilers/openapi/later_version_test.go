package openapi_test

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

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// laterVersionCase is the conformance spec writing every construct a later
// minor added, in a document declaring 3.0.3.
const laterVersionCase = "later-version-constructs"

// assertLaterVersionConstructs pins a sample of the case's readings, one per
// kind of object, each as a 3.1 or 3.2 document lowers it, and that nothing is
// called undefined. TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem
// holds the whole document to that.
func assertLaterVersionConstructs(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "production", doc.Servers[0].Name.Source)
	require.NotEmpty(t, doc.TagDefs)
	assert.Equal(t, "catalog", doc.TagDefs[0].Parent)

	search, ok := opByName(doc, "searchBooks")
	require.True(t, ok)
	assert.Equal(t, "QUERY", search.Bindings.HTTP[0].Method)
	purge, ok := opByName(doc, "purgeBooks")
	require.True(t, ok)
	assert.Equal(t, "PURGE", purge.Bindings.HTTP[0].Method)
	list, ok := opByName(doc, "listBooks")
	require.True(t, ok)
	assert.Equal(t, ir.HTTPLocationQuerystring, list.Bindings.HTTP[0].ParamBindings[0].Location)
	assert.Equal(t, "The books.", list.Responses[0].Docs.Summary)
	require.Len(t, list.Errors, 1)
	assert.Equal(t, ir.Docs{Summary: "No such book.", Description: "not found"}, list.Errors[0].Docs,
		"a Reference Object's summary folds over the target's docs, as in 3.1")
	assert.Equal(t, ir.ReasonOutOfScope, doc.Unmodeled["openapi:jsonSchemaDialect"].Reason)

	item, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Item")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, item.XML)
	assert.Equal(t, "element", item.XML.NodeType)
	require.NotNil(t, item.Discriminator)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Novel"), item.Discriminator.Default)

	assert.False(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == diag.UnknownObjectKey }),
		"a construct a later minor defines is not an undefined key")
	for _, d := range diags {
		if d.Code == diag.LaterVersionConstruct {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
		}
	}
}

// TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem is the rule as a
// test: the case declared at each minor the compiler reads lowers to one
// document, and its diagnostics differ only by the warnings naming a construct
// added after the declared minor. 3.1 is owed fewer of them than 3.0, and 3.2
// none.
func TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(conformanceDir, laterVersionCase+".yaml"))
	require.NoError(t, err)
	compile := func(version string) (string, []string, []string) {
		t.Helper()
		src := strings.Replace(string(data), "openapi: 3.0.3", "openapi: "+version, 1)
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		var later, others []string
		for _, d := range diags {
			line := fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer)
			if d.Code == diag.LaterVersionConstruct {
				later = append(later, line)
			} else {
				others = append(others, line)
			}
		}
		return withoutVersion(t, doc), later, others
	}
	body32, later32, others32 := compile("3.2.0")
	assert.Empty(t, later32)
	body31, later31, others31 := compile("3.1.0")
	body30, later30, others30 := compile("3.0.3")

	assert.Equal(t, body32, body31, "a 3.1 document lowers as a 3.2 one")
	assert.Equal(t, body32, body30, "a 3.0 document lowers as a 3.2 one")
	// A rule a minor adds binds a document declaring it, and no older one, whose
	// construct the later-version warning already names.
	until31 := slices.DeleteFunc(slices.Clone(others32), func(line string) bool {
		return strings.Contains(line, "validation-mutually-exclusive-fields")
	})
	assert.Equal(t, until31, others31)
	assert.Equal(t, until31, others30)
	assert.NotEmpty(t, later31)
	assert.Subset(t, later30, later31, "what 3.2 added is owed to 3.0 as to 3.1")
	assert.Greater(t, len(later30), len(later31), "and what 3.1 added to 3.0 alone")
}

// withoutVersion encodes doc with what records the declared version set aside:
// its source entries, which name the format and hash the bytes, and its
// diagnostics, which the test compares apart.
func withoutVersion(t *testing.T, doc *ir.Document) string {
	t.Helper()
	stripped := *doc
	stripped.Sources = nil
	stripped.Diagnostics = nil
	out, err := json.Marshal(&stripped, json.Deterministic(true))
	require.NoError(t, err)
	return string(out)
}
