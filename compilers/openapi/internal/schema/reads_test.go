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
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// readDisagreementClasses are the census sites, node kinds, keys and verdicts
// at which the census and the reads of the builds it judges still disagree,
// each of which some document under testdata or a core case reaches.
var readDisagreementClasses = []string{
	"own\tliteral\ttype\thomed, unread",
}

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
	for _, src := range testdataSources(t) {
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
