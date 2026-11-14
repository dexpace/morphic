// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// compile runs the whole public compiler pipeline over src under opts. It
// reaches back through openapi deliberately: a page is read through the
// registry every other lowering fills, and only a whole compile fills it.
// That reach is also why openapitest cannot hold it — see that package's doc
// comment.
func compile(t *testing.T, src string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// listing is a document of one GET operation, listThings, taking the query
// parameters params and answering 200 with the JSON body, each written as a
// YAML flow sequence or mapping.
func listing(params, body string) string {
	return fmt.Sprintf(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: %s
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: %s
`, params, body)
}

// listOp compiles src under opts and returns its listThings operation, the
// document and every diagnostic.
func listOp(t *testing.T, src string, opts openapi.Options) (ir.Operation, *ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := compile(t, src, opts)
	return openapitest.FindOp(t, doc, "listThings"), doc, diags
}

// describe renders op's Pagination by the names it reads, a path as the wire
// names of its members joined by dots, so a case states what a walk reads in
// the document's own words: "-" for no Pagination.
func describe(doc *ir.Document, op ir.Operation) string {
	pg := op.Pagination
	if pg == nil {
		return "-"
	}
	params := map[ir.ParamID]string{}
	for _, p := range op.Params {
		params[p.ID] = p.Name.Source
	}
	members := map[ir.PropID]string{}
	for _, td := range doc.Types {
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				members[p.ID] = p.EffectiveWireName()
			}
		}
	}
	input := func(in *ir.InputPath) string {
		if in == nil {
			return "-"
		}
		return params[*in.Param]
	}
	path := func(p *ir.PropPath) string {
		if p == nil {
			return "-"
		}
		names := make([]string, len(p.Segments))
		for i, s := range p.Segments {
			names[i] = members[s]
		}
		return "/" + strings.Join(names, "/")
	}
	return fmt.Sprintf("%s cursor=%s limit=%s items=%s next=%s count=%s pages=%s stop=%v", pg.Strategy,
		input(pg.InputCursor), input(pg.InputLimit), path(pg.Items), path(pg.NextCursor), path(pg.TotalCount),
		path(pg.TotalPages), pg.Stop)
}

// undecided returns the messages of the openapi/undecided-pagination findings
// among diags.
func undecided(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.UndecidedPagination {
			out = append(out, d.Message)
		}
	}
	return out
}
