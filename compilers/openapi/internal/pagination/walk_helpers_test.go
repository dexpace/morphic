// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// bodyProps is the ID prefix of the members of the JSON body the success
// response of the operation method at path declares.
func bodyProps(path, method string) string {
	return "p/openapi/paths/~1" + strings.ReplaceAll(path, "/", "~1") + "/" + method + "/responses/200/content/application~1json/schema/properties"
}

// propPath is a path of the members whose IDs are ids, in a response body.
func propPath(ids ...string) *ir.PropPath {
	return &ir.PropPath{Segments: memberIDs(ids)}
}

// inputPath is a path of the members whose IDs are ids, walked from the
// request body, or from the parameter param when it is not empty.
func inputPath(param string, ids ...string) *ir.InputPath {
	in := &ir.InputPath{Segments: memberIDs(ids)}
	if param != "" {
		id := ir.ParamID(param)
		in.Param = &id
	}
	return in
}

// memberIDs converts ids, nil for none: a path walking nothing holds no
// segments, which is not the same value as an empty list of them.
func memberIDs(ids []string) []ir.PropID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]ir.PropID, 0, len(ids))
	for _, id := range ids {
		out = append(out, ir.PropID(id))
	}
	return out
}

// queryParam is the ID of the query parameter name of the GET at path.
func queryParam(path, name string) string {
	return "param/openapi/paths/~1" + path + "/get/parameters/" + name + "/query"
}

// inEitherOrder returns src and src with every mapping's entries reversed:
// the same document with each declaration after the ones it was before.
func inEitherOrder(t *testing.T, src string) []string {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	stack := []*yaml.Node{&root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == yaml.MappingNode {
			pairs := slices.Collect(slices.Chunk(n.Content, 2))
			slices.Reverse(pairs)
			n.Content = slices.Concat(pairs...)
		}
		stack = append(stack, n.Content...)
	}
	out, err := yaml.Marshal(&root)
	require.NoError(t, err)
	return []string{src, string(out)}
}

// undecidedAt returns the messages of the openapi/undecided-pagination
// findings reported at pointer.
func undecidedAt(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == "openapi/undecided-pagination" && string(d.Provenance.Pointer) == pointer {
			out = append(out, d.Message)
		}
	}
	return out
}
