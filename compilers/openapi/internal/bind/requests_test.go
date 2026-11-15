// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/ir/irtest"
)

// requestFixture is a multi-document binding: the source's schemas, the
// documents a reader serves by name, and the config it is bound under.
type requestFixture struct {
	name    string
	cfg     bind.Config
	schemas string
	docs    map[string]string
}

// requestFixtures are the multi-document shapes whose reads a change to when
// documents are read may move: documents declaring one another's URIs, a
// file declaring a path or a URL another reference names, and the chains of
// declaring documents, by path and by URL.
func requestFixtures() []requestFixture {
	held := map[string]string{
		"https://x.test/u1": "S: {$id: 'https://x.test/u2', type: string}\n",
		"https://x.test/u2": "type: integer\n",
	}
	bundle := map[string]string{"/lib/bundle.yaml": ""}
	var uses strings.Builder
	for k := range 5 {
		bundle["/lib/bundle.yaml"] += fmt.Sprintf("s%d: {$id: 'https://x.test/s%d.yaml', type: string}\n", k, k)
		fmt.Fprintf(&uses, "    U%d: {$ref: 'https://x.test/s%d.yaml'}\n", k, k)
	}
	fixtures := make([]requestFixture, 0, 8)
	fixtures = append(fixtures, []requestFixture{
		{"held documents declaring another's URI", self,
			"    A: {$ref: 'https://x.test/u1#/S'}\n    B: {$ref: 'https://x.test/u2'}\n", held},
		{"held documents, the other order", self,
			"    B: {$ref: 'https://x.test/u2'}\n    A: {$ref: 'https://x.test/u1#/S'}\n", held},
		{"a path a document declares", resolving, "    A: {$ref: 'a.yaml'}\n    B: {$ref: 'b.yaml#/T'}\n",
			map[string]string{"/a.yaml": "{type: boolean}\n", "/b.yaml": "T: {type: string}\nS: {$id: 'a.yaml', type: integer}\n"}},
		{"URLs a file declares", resolving, uses.String() + "    L: {$ref: 'lib/bundle.yaml#/s0'}\n", bundle},
		{"URLs a file declares, the file first", resolving, "    L: {$ref: 'lib/bundle.yaml#/s0'}\n" + uses.String(), bundle},
		{"a URL a file declares and a server serves", resolving,
			"    A: {$ref: 'local.yaml#/Z'}\n    B: {$ref: 'https://x.test/pet.yaml'}\n    C: {$ref: 'https://x.test/gone.yaml'}\n",
			map[string]string{
				"/local.yaml":             "Z: {$id: 'https://x.test/pet.yaml', type: object}\nY: {$id: 'https://x.test/gone.yaml', type: integer}\n",
				"https://x.test/pet.yaml": "{type: string}\n",
			}},
	}...)
	for _, base := range []string{"", "https://x.test/"} {
		src, docs := ladder(5, base)
		cfg := resolving
		cfg.Self = base + "spec.yaml"
		if base == "" {
			cfg.Self, docs = "/spec.yaml", rooted(docs)
		}
		fixtures = append(fixtures, requestFixture{"a ladder of 5 at " + strconv.Quote(base), cfg,
			strings.TrimPrefix(src, head+"components:\n  schemas:\n"), docs})
	}
	return fixtures
}

// requested binds each fixture through a reader recording each request, and
// returns a line per fixture and name read, with how often it was read.
func requested(t *testing.T) []string {
	t.Helper()
	lines := make([]string, 0, 32)
	for _, f := range requestFixtures() {
		var log askLog
		bound(t, head+"components:\n  schemas:\n"+f.schemas, log.opening(t, f.cfg, f.docs))
		count := map[string]int{}
		for _, name := range log.names() {
			count[name]++
		}
		for _, name := range slices.Sorted(maps.Keys(count)) {
			lines = append(lines, fmt.Sprintf("%s\t%s\t%d", f.name, name, count[name]))
		}
	}
	return lines
}

// TestBind_RequestsWhatMainRequests holds the documents each fixture's
// binding asks its reader for to those it asked for before reads were held
// and drained in tiers (testdata/requests.golden.txt, recorded then): a
// binding may ask for less, never for a document, or as often, as it did not.
// A sweep of specs reads no URL, so this is what sees a request added.
func TestBind_RequestsWhatMainRequests(t *testing.T) {
	t.Parallel()
	const path = "testdata/requests.golden.txt"
	got := requested(t)
	if irtest.Update() {
		require.NoError(t, os.WriteFile(path, []byte(strings.Join(got, "\n")+"\n"), 0o600))
		return
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	before := map[string]int{}
	for line := range strings.Lines(string(data)) {
		key, times := splitCount(t, strings.TrimSuffix(line, "\n"))
		before[key] = times
	}
	for _, line := range got {
		key, times := splitCount(t, line)
		was, asked := before[key]
		assert.True(t, asked, "%s: requested now and not before", key)
		assert.LessOrEqual(t, times, was, "%s: requested more often than before", key)
	}
}

// splitCount splits a line requested writes into its fixture and name, and
// its count.
func splitCount(t *testing.T, line string) (string, int) {
	t.Helper()
	at := strings.LastIndexByte(line, '\t')
	require.Positive(t, at, line)
	times, err := strconv.Atoi(line[at+1:])
	require.NoError(t, err, line)
	return line[:at], times
}
