// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir/irtest"
)

// formerWhole is the read admissionOf replaced, kept to hold it to: recursive,
// it reads js afresh at each ask, spending w's budget per schema, and is cut
// short where the budget runs out, at a $dynamicRef the bindings read as no
// $ref and at a $ref landing on no schema.
func (w *admission) formerWhole(js *statement.Statement) admitted {
	if !w.reads.Spend(1) || js.Truth == statement.Unwritten {
		return admitted{kinds: kindAny}
	}
	if js.Truth != statement.Stated {
		if js.Truth.ReadsFalse() {
			return admitted{}
		}
		return admitted{kinds: kindAny}
	}
	if declaresDynamicRef(js.Schema) && js.Ref == nil {
		w.reads.Exhaust()
	}
	out := w.written(js.Schema, definesNullableAt(w.c, js))
	for _, b := range js.AllOf {
		out = out.and(w.formerWhole(w.c.Statements().Sub(b)))
	}
	choice := choiceIn(js.Schema)
	for _, branches := range [][]statement.Sub{choice.OneOf, choice.AnyOf} {
		if len(branches) == 0 {
			continue
		}
		union := w.formerWhole(w.c.Statements().Sub(branches[0]))
		for _, b := range branches[1:] {
			union = union.or(w.formerWhole(w.c.Statements().Sub(b)))
		}
		out = out.and(union)
	}
	if js.Ref == nil {
		return out
	}
	next, _ := declaredOf(w.c, js)
	if next == nil {
		w.reads.Exhaust()
		return out
	}
	return out.and(w.formerWhole(next))
}

// specsUnder returns the OpenAPI 3 documents under the repository's testdata,
// by path, each as its bytes.
func specsUnder(t *testing.T) map[string][]byte {
	t.Helper()
	root := filepath.Join(irtest.RepoRoot(t), "testdata")
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".json") {
			return err
		}
		src, err := os.ReadFile(path)
		if err == nil && (bytes.Contains(src, []byte("openapi: 3")) || bytes.Contains(src, []byte(`"openapi": "3`))) {
			out[path] = src
		}
		return err
	})
	require.NoError(t, err)
	return out
}

// loweredOrNot returns the fixture over src, or nil where it does not load.
func loweredOrNot(t *testing.T, src []byte) *Lowerer {
	t.Helper()
	loaded, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(string(src)), load.Options{})
	if err != nil || loaded == nil {
		return nil
	}
	return LowererOver(service.New(0, loaded, service.Policy{}))
}

// statementsOfDocument calls visit with the statement of each schema the
// root document of l writes, wherever it is written.
func statementsOfDocument(l *Lowerer, visit func(at jsontext.Pointer, st *statement.Statement)) {
	var walk func(n *yaml.Node, at jsontext.Pointer, depth int)
	walk = func(n *yaml.Node, at jsontext.Pointer, depth int) {
		if n == nil || depth > 512 {
			return
		}
		if st := l.Ctx.Statements().At(bind.Position{Doc: bind.Source, Pointer: at}); st.Held() {
			visit(at, st)
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(n.Content[i+1], at.AppendToken(n.Content[i].Value), depth+1)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, at.AppendToken(strconv.Itoa(i)), depth+1)
			}
		default:
		}
	}
	walk(nodeview.DocumentRoot(l.Doc.GetCore().GetRootNode()), "", 0)
}

// TestAdmissionOf_AgreesWithAReadOfItsOwn holds what admissionOf remembers of
// every schema the documents under testdata write to what a read of that
// schema alone reaches, wherever neither is cut short: a remembered answer is
// the one its reader would have read.
func TestAdmissionOf_AgreesWithAReadOfItsOwn(t *testing.T) {
	t.Parallel()
	compared := 0
	for path, src := range specsUnder(t) {
		l := loweredOrNot(t, src)
		if l == nil {
			continue
		}
		statementsOfDocument(l, func(at jsontext.Pointer, st *statement.Statement) {
			r := admissionOf(l.Ctx, st)
			former := newAdmission(l.Ctx)
			want := former.formerWhole(st)
			if r.cut || r.loose || r.reads > maxAdmissionReads || r.members > maxAdmissionMembers || former.incomplete() {
				return
			}
			compared++
			assert.Equal(t, fmt.Sprint(want), fmt.Sprint(r.admitted), "%s#%s", path, at)
		})
	}
	assert.Greater(t, compared, 1000, "the corpus reaches what it compares")
}

// TestAdmissionOf_ACycleReadsAlikeWhicheverStatementIsAskedFirst holds the
// reading of every schema of a cycle, and of one reaching it and one beside
// it, to one answer whichever is asked first: a read is never remembered from
// inside an open cycle.
func TestAdmissionOf_ACycleReadsAlikeWhicheverStatementIsAskedFirst(t *testing.T) {
	t.Parallel()
	const n = 64
	spec := openapitest.ComponentSpecVer("3.1.0", clique(n)+
		"    Reaches: {allOf: [{$ref: '#/components/schemas/K7'}, {type: object}]}\n"+
		"    Beside: {allOf: [{type: string}, {minLength: 1}]}\n")
	names := make([]string, 0, 2+n)
	names = append(names, "Reaches", "Beside")
	for i := range n {
		names = append(names, fmt.Sprintf("K%d", i))
	}
	readAll := func(first string) map[string]string {
		l, _ := LoweredFor(t, spec)
		statementOf := func(name string) *statement.Statement {
			return l.Ctx.Statements().Of(l.Doc.Components.Schemas.GetOrZero(name))
		}
		admissionOf(l.Ctx, statementOf(first))
		out := map[string]string{}
		for _, name := range names {
			out[name] = fmt.Sprint(*admissionOf(l.Ctx, statementOf(name)))
		}
		return out
	}
	want := readAll("Beside")
	assert.Contains(t, want["Reaches"], "true", "a schema reaching the cycle is cut")
	for _, first := range []string{"Reaches", "K0", "K31", "K63"} {
		assert.Equal(t, want, readAll(first), "asked %s first", first)
	}
}

// TestAdmissionOf_VisitsEachStatementAndEdgeOnce holds a run's walks to one
// visit of each statement they settle and one of each of its conjunct edges,
// on a cycle and on a chain each read at three sizes, so the cost of the read
// grows with the document and not with its paths.
func TestAdmissionOf_VisitsEachStatementAndEdgeOnce(t *testing.T) {
	t.Parallel()
	chain := func(n int) string {
		var b strings.Builder
		b.WriteString("    C0: {type: string}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "    C%d: {allOf: [{$ref: '#/components/schemas/C%d'}, {$ref: '#/components/schemas/C%d'}]}\n",
				i, i-1, max(0, i-2))
		}
		return b.String()
	}
	for _, tc := range []struct {
		name  string
		shape func(int) string
	}{{"clique", clique}, {"chain", chain}} {
		for _, n := range []int{16, 32, 64} {
			l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", tc.shape(n)))
			for _, js := range l.Doc.Components.Schemas.All() {
				admissionOf(l.Ctx, l.Ctx.Statements().Of(js))
			}
			store := admissionStore(l.Ctx)
			edges := 0
			for st := range store.read {
				edges += len(conjunctsOf(l.Ctx, st))
			}
			assert.Equal(t, len(store.read), store.visits, "%s of %d: each statement once", tc.name, n)
			assert.Equal(t, edges, store.edges, "%s of %d: each edge once", tc.name, n)
		}
	}
}
