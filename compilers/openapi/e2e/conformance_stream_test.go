// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// corpusWith compiles one corpus spec under opts, for the assertions of a case
// whose conformance document is the default compile and whose subject needs
// another.
func corpusWith(t *testing.T, name string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: data}}, compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// markersOf splits a marker list into its names.
func markersOf(m ir.Inferred) []string {
	if m == "" {
		return nil
	}
	return strings.Split(string(m), ",")
}

// streamPromotions maps the two stream targets to the keys stream-terminator
// writes.
func streamPromotions() openapi.ExtensionPromotions {
	return openapi.ExtensionPromotions{Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
		openapi.TargetStreamTerminator: {{Key: "x-speakeasy-sse-sentinel"}},
		openapi.TargetStreamCondition:  {{Key: "x-stream-when"}},
	}}
}

// assertStreamTerminator pins what the case's keys fill. By default the
// sentinel the streaming content states is the stream's terminator, kept
// there beside the marker saying a promotion read it, while the content's own
// stream marker and the operation's are unchanged and no condition is read.
// Under a mapping naming the operation's key as well, the input it selects
// the stream by is a condition, recorded on the operation. With promotion off,
// every key stays where it was written and nothing is filled.
func assertStreamTerminator(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const (
		key       = "openapi:x-speakeasy-sse-sentinel"
		promoted  = ir.Inferred("extension-promotion")
		streaming = ir.Inferred("streaming-inference")
	)
	contentsOf := func(op ir.Operation) []ir.Content {
		require.Len(t, op.Responses, 1)
		require.NotNil(t, op.Responses[0].Payload)
		require.Len(t, op.Responses[0].Payload.Contents, 2)
		return op.Responses[0].Payload.Contents
	}

	op, ok := opByName(doc, "chat")
	require.True(t, ok)
	require.NotNil(t, op.ResponseStream)
	require.NotNil(t, op.ResponseStream.Terminator)
	assert.Equal(t, "[DONE]", *op.ResponseStream.Terminator)
	assert.Nil(t, op.ResponseStream.Condition, "no default source names a condition")
	contents := contentsOf(op)
	assert.Empty(t, contents[0].Inferred, "the content that does not stream read no key")
	assert.Equal(t, promoted, contents[1].Inferred, "the content whose key was read records it")
	require.NotNil(t, contents[1].Stream)
	assert.Equal(t, streaming, contents[1].Stream.Inferred, "how it is known to stream is unchanged")
	assert.Equal(t, streaming, op.Provenance.Inferred, "the operation records no promotion for a key it did not read")
	assert.Contains(t, contents[1].Unmodeled, key, "the key is kept")
	assert.Empty(t, diags)

	mapped, mappedDiags := corpusWith(t, "stream-terminator", openapi.Options{Promotions: streamPromotions()})
	mop, ok := opByName(mapped, "chat")
	require.True(t, ok)
	require.NotNil(t, mop.ResponseStream)
	require.NotNil(t, mop.ResponseStream.Terminator)
	assert.Equal(t, "[DONE]", *mop.ResponseStream.Terminator)
	cond := mop.ResponseStream.Condition
	require.NotNil(t, cond)
	assert.Nil(t, cond.Input.Param, "the input is a member of the request body")
	assert.Equal(t, []ir.PropID{"p/openapi/paths/~1chat/post/requestBody/content/application~1json/schema/properties/stream"},
		cond.Input.Segments)
	assert.Equal(t, ir.ValueBool, cond.Value.Kind)
	assert.True(t, cond.Value.Bool)
	assert.ElementsMatch(t, []string{"streaming-inference", "extension-promotion"}, markersOf(mop.Provenance.Inferred))
	assert.Contains(t, mop.Unmodeled, "openapi:x-stream-when", "the key is kept")
	assert.Empty(t, mappedDiags)

	off, offDiags := corpusWith(t, "stream-terminator", openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}})
	oop, ok := opByName(off, "chat")
	require.True(t, ok)
	require.NotNil(t, oop.ResponseStream)
	assert.Nil(t, oop.ResponseStream.Terminator, "promotion off fills nothing")
	assert.Nil(t, oop.ResponseStream.Condition)
	offContents := contentsOf(oop)
	assert.Empty(t, offContents[1].Inferred)
	assert.Contains(t, offContents[1].Unmodeled, key, "the key stays as written")
	assert.Contains(t, oop.Unmodeled, "openapi:x-stream-when")
	assert.Equal(t, streaming, oop.Provenance.Inferred)
	assert.Empty(t, offDiags)
}

// assertPathPrefixQuery pins how path-prefix grouping reads a Paths key
// carrying a query: it names its group by the path alone, so the key joins the
// key without a query and one whose path is the root joins the root's group,
// the query stays in the URI template, and the two keys are reported as the
// invalid path keys they are, by the load and not by the grouping.
func assertPathPrefixQuery(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
	assert.Equal(t, []string{"/paths/~1?probe=1", "/paths/~1users?role=admin"},
		sortedPointers(diags, "openapi/invalid-path-key"))

	doc, grouped := corpusWith(t, "path-prefix-query", openapi.Options{Grouping: openapi.GroupByPathPrefix})
	assert.Len(t, grouped, 2, "no report beside the two path keys: %+v", grouped)
	require.Len(t, doc.Services, 1)
	groups := doc.Services[0].Groups
	require.Len(t, groups, 2)

	assert.Equal(t, ir.GroupID("g/path-prefix/users"), groups[0].ID)
	require.Len(t, groups[0].Operations, 2)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1users/get"), groups[0].Operations[0].ID)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1users?role=admin/get"), groups[0].Operations[1].ID)
	assert.Equal(t, "/users?role=admin", groups[0].Operations[1].Bindings.HTTP[0].URITemplate,
		"the query stays in the template")

	assert.Equal(t, ir.GroupID("g/path-prefix"), groups[1].ID)
	assert.Equal(t, ir.Naming{Hint: "empty"}, groups[1].Name)
	require.Len(t, groups[1].Operations, 1)
	assert.Equal(t, ir.OpID("op/openapi/paths/~1?probe=1/get"), groups[1].Operations[0].ID)
}

// sortedPointers returns the pointers of the diagnostics of code, as strings
// in sorted order.
func sortedPointers(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}
