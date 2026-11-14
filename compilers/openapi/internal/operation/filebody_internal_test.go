// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestLeastOf_StopsAtOther holds leastOf to the least of its items and to the
// items it reads: none past the first reading as other, which no later one can
// lower, so a wide union one branch decides is read no further than that
// branch. No items read as the top.
func TestLeastOf_StopsAtOther(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		items []fileReading
		want  fileReading
		reads int
	}{
		{"none", nil, readsBinary, 0},
		{"raw bytes alone", []fileReading{readsBinary, readsBinary}, readsBinary, 2},
		{"the least of open readings", []fileReading{readsUntyped, readsBinary, readsString, readsUntyped}, readsString, 4},
		{"other first", []fileReading{readsOther, readsBinary, readsUntyped}, readsOther, 1},
		{"other among them", []fileReading{readsUntyped, readsOther, readsString, readsBinary}, readsOther, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := 0
			c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
			got := leastOf(c, tc.items, func(_ lowering.Ctx, r fileReading) fileReading {
				reads++
				return r
			})
			assert.Equal(t, tc.want, got, "least")
			assert.Equal(t, tc.reads, reads, "items read")
		})
	}
}

// unionSchemas is a document whose component U is an anyOf of branches, and
// whose components C0 to C<uses-1> each name it through a $ref.
func unionSchemas(branches []string, uses int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n")
	fmt.Fprintf(&b, "    U: {anyOf: [%s]}\n", strings.Join(branches, ", "))
	for i := range uses {
		fmt.Fprintf(&b, "    C%d: {$ref: '#/components/schemas/U'}\n", i)
	}
	return b.String()
}

// readAs reads the component name of the document l holds as a body, or as a
// part when part is set, through the branch reader of that reading, counting
// each branch read in reads.
func readAs(t *testing.T, l *lowerer, name string, part bool, reads *int) fileReading {
	t.Helper()
	js, ok := l.ctx.Doc.Components.Schemas.Get(name)
	require.True(t, ok, name)
	read := readBranch
	if part {
		read = readPartBranch
	}
	return readSchema(l.ctx, js, branchReading{part: part, read: func(c lowering.Ctx, branch *oas3.JSONSchema[oas3.Referenceable]) fileReading {
		*reads++
		return read(c, branch)
	}})
}

// TestReadSchema_ReadsAUnionOncePerRun holds the reads of a union's branches to
// one for each way the union is read, however many schemas name it: W branches
// make W reads whether one content or eight names the union, and W more when
// the union is read as a part, whose branches are read as a part's are. The two
// ways answer differently for a union holding a list of bytes, which a part
// reads through its elements and a body does not, so a reading kept for one way
// must not answer for the other, in either order.
func TestReadSchema_ReadsAUnionOncePerRun(t *testing.T) {
	t.Parallel()
	const width = 6
	strs := make([]string, width)
	for i := range strs {
		strs[i] = "{type: string}"
	}
	for _, uses := range []int{1, 8} {
		t.Run(fmt.Sprintf("%d contents", uses), func(t *testing.T) {
			t.Parallel()
			l, _ := loweredFor(t, unionSchemas(strs, uses))
			reads := 0
			for i := range uses {
				readAs(t, l, fmt.Sprintf("C%d", i), false, &reads)
			}
			assert.Equal(t, width, reads, "the union is read once as a body, whoever names it")
			readAs(t, l, "C0", true, &reads)
			assert.Equal(t, 2*width, reads, "and once more as a part")
			readAs(t, l, "C0", true, &reads)
			readAs(t, l, "C0", false, &reads)
			assert.Equal(t, 2*width, reads, "and no more for a way it has been read")
		})
	}

	branches := []string{"{type: string, format: binary}", "{type: array, items: {type: string, format: binary}}"}
	fresh := func(part bool) fileReading {
		l, _ := loweredFor(t, unionSchemas(branches, 1))
		return readAs(t, l, "C0", part, new(int))
	}
	body, part := fresh(false), fresh(true)
	require.NotEqual(t, body, part, "the union reads differently as a body and as a part")
	for _, order := range [][]bool{{false, true}, {true, false}} {
		l, _ := loweredFor(t, unionSchemas(branches, 1))
		for _, asPart := range order {
			want := body
			if asPart {
				want = part
			}
			assert.Equal(t, want, readAs(t, l, "C0", asPart, new(int)), "as a part: %v, after %v", asPart, order)
		}
	}
}
