// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// apiTree returns the tree of a JSON API description of paths paths, each with
// two operations of four parameters and a response, about a million nodes at
// the 6,000 paths the benchmarks read: the size of a large real description.
func apiTree(b *testing.B, paths int) *yaml.Node {
	b.Helper()
	var text strings.Builder
	text.WriteString(`{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {`)
	for i := range paths {
		if i > 0 {
			text.WriteByte(',')
		}
		fmt.Fprintf(&text, `"/p%d": {`, i)
		for j, method := range []string{"get", "post"} {
			if j > 0 {
				text.WriteByte(',')
			}
			fmt.Fprintf(&text, `"%s": {"operationId": "op%d%s", "parameters": [`, method, i, method)
			for k, in := range []string{"query", "path", "query", "header"} {
				if k > 0 {
					text.WriteByte(',')
				}
				fmt.Fprintf(&text, `{"name": "a%d", "in": "%s", "required": %t, "schema": {"type": "string", "maxLength": %d}}`,
					k, in, k%2 == 0, k*8)
			}
			text.WriteString(`], "responses": {"200": {"description": "ok", "content": {"application/json": ` +
				`{"schema": {"type": "object", "properties": {"id": {"type": "integer"}, "name": {"type": "string"}}}}}}}}`)
		}
		text.WriteByte('}')
	}
	text.WriteString("}}")
	return readTree(b, text.String())
}

// benchmarkSelect evaluates query over root once per iteration and reports the
// steps it spends.
func benchmarkSelect(b *testing.B, query string, root *yaml.Node) {
	b.Helper()
	q, err := jsonpath.Parse(query)
	require.NoError(b, err)
	spent, selected := 0, 0
	b.ReportAllocs()
	for b.Loop() {
		steps := budget.New(budget.Unbounded())
		got, err := q.Select(b.Context(), nodeview.New(), root, &steps)
		if err != nil {
			b.Fatal(err)
		}
		spent, selected = steps.Spent(), len(got)
	}
	b.ReportMetric(float64(spent), "steps/op")
	b.ReportMetric(float64(selected), "nodes/op")
}

// BenchmarkSelect_EveryNodeOfALargeDescription measures $..* over about a
// million nodes: a descendant segment visiting each node once.
func BenchmarkSelect_EveryNodeOfALargeDescription(b *testing.B) {
	benchmarkSelect(b, "$..*", apiTree(b, 6000))
}

// BenchmarkSelect_AFilterOverALargeDescription measures a filter over every
// operation's parameters of the same description.
func BenchmarkSelect_AFilterOverALargeDescription(b *testing.B) {
	benchmarkSelect(b, "$.paths.*.*.parameters[?@.in == 'query']", apiTree(b, 6000))
}
