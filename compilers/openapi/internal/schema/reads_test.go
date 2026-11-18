// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"flag"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir"
)

// readDisagreementClasses are the census sites, node kinds, keys and verdicts
// at which the census and the reads of the builds it judges still disagree,
// each of which some document under testdata or a core case reaches.
var readDisagreementClasses = []string{}

// TestCensus_AgreesWithWhatTheBuildsRead holds the census, at every site it
// judges a key of the component schemas of each document under testdata and
// of each core case, to what the builds it judges say they read: it keeps a
// key no build read and homes one a build did. A class it disagrees at is
// listed in readDisagreementClasses until the builder reads what the census
// credits it with.
func TestCensus_AgreesWithWhatTheBuildsRead(t *testing.T) {
	t.Parallel()
	found := map[string]string{}
	add := func(where string, lines []string) {
		for _, line := range lines {
			fields := strings.Split(line, "\t")
			class := strings.Join(fields[:4], "\t")
			if _, seen := found[class]; !seen {
				found[class] = where + " " + fields[4]
			}
		}
	}
	for _, src := range append(testdataSources(t), readEdges()...) {
		if lines, loaded := schema.ReadDisagreements(t, src); loaded {
			add(src.Path, lines)
		}
	}
	cases := openapicases.New()
	each, err := harness.Each(cases)
	require.NoError(t, err)
	core := harness.Core(cases)
	for _, at := range each {
		doc, writes := cases.Write(at)
		if !writes || !*fullGrid && !core(at) {
			continue
		}
		if lines, loaded := schema.ReadDisagreements(t, doc.Sources[0]); loaded {
			add("case", lines)
		}
	}
	for _, class := range readDisagreementClasses {
		assert.Contains(t, found, class, "a listed class no input reaches any longer")
	}
	for _, class := range slices.Sorted(maps.Keys(found)) {
		assert.Contains(t, readDisagreementClasses, class, "first met at %s", found[class])
	}
}

// TestCensus_EdgesCompileAsTheyDid pins what the documents readEdges adds
// compile to, which the census decides and the builds' reads must keep: the
// empty pattern no variant holds is kept on the union, and the type an enum's
// members all are values of says nothing the Enum does not.
func TestCensus_EdgesCompileAsTheyDid(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, string(readEdges()[0].Data))
	pattern := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "EmptyPattern"))]
	require.NotNil(t, pattern)
	assert.Equal(t, ir.KindUnion, pattern.Kind())
	assert.Contains(t, pattern.Common().Unmodeled, "openapi:pattern", "no variant holds the empty pattern")
	enum, ok := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "EnumOfTypes"))].(*ir.Enum)
	require.True(t, ok, "the members are of one kind")
	assert.Equal(t, ir.PrimString, enum.ValueType)
	assert.NotContains(t, enum.Unmodeled, "openapi:type", "every member is a value of the types")
}

// readEdges returns the documents of what reaches a census site no document
// under testdata or core case does: an empty pattern beside a type list whose
// string variant is a Scalar of its own, and an enum whose members are all
// values of the several types beside it.
func readEdges() []compilers.Source {
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    EmptyPattern: {type: [string, integer], format: x-text, pattern: ""}
    EnumOfTypes: {type: [string, integer], enum: [a, b]}
`
	return []compilers.Source{{Path: "edges.yaml", Data: []byte(spec)}}
}

// fullGrid widens the cases TestCensus_AgreesWithWhatTheBuildsRead lowers from
// the core cases to every case, as make cases does.
var fullGrid = flag.Bool("full", false, "lower every case, not the core cases alone")

// testdataSources returns each OpenAPI document under the repository's
// testdata, read as a source.
func testdataSources(t *testing.T) []compilers.Source {
	t.Helper()
	var out []compilers.Source
	root := filepath.Join("..", "..", "..", "..", "testdata")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(path)) {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "openapi") || strings.Contains(string(data), "swagger") {
			out = append(out, compilers.Source{Path: path, Data: data})
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	return out
}
