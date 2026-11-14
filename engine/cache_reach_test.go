// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
)

// TestWithCache_ABoundedReadIsRecordedWithItsReachAndReplayed pins the cache
// of a compile whose fetched document names another: the second document is
// read at the reach of the first (a loopback server states loopback, which
// reaches itself), the entry lists that read with its reach, and a second run
// is answered from the entry, replaying the bounded read against the server.
func TestWithCache_ABoundedReadIsRecordedWithItsReachAndReplayed(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int64
	docs := map[string]string{
		"/a.yaml": "X: {$ref: 'b.yaml#/Y'}\n",
		"/b.yaml": "Y: {type: string}\n",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		body, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	spec := writeSpec(t, strings.ReplaceAll(refSpec, "other.yaml#/components/schemas/Pet", server.URL+"/a.yaml#/X"))
	opts := engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}}
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")

	first, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	require.False(t, first.Failed, "%v", first.Diagnostics)
	require.Len(t, first.Sources, 3, "the fixture reads the chain of two fetched documents")

	_, entry := store.only(t)
	head, _, _ := bytes.Cut(bytes.SplitN(entry, []byte{'\n'}, 2)[1], []byte{'\n'})
	assert.Contains(t, strings.ToLower(string(head)), `"reach"`, "the record keeps the bound each read was made under")

	before := fetches.Load()
	again, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counter.compiles.Load(), "the second run is answered from the entry")
	assert.Greater(t, fetches.Load(), before, "and replays each read, the bounded one included")
	assertSameRun(t, first, again)
}

// TestWithCache_ARunThatRequestsNothingLeavesASoundEntry pins that a compile
// whose URLs are all answered by the declarations of files it reads, with a
// server that serves nothing and a document budget of one, requests nothing
// and is recorded as sound: its second run is answered from the cache.
func TestWithCache_ARunThatRequestsNothingLeavesASoundEntry(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	var bundle, uses strings.Builder
	for _, k := range []string{"0", "1", "2", "3", "4"} {
		bundle.WriteString("s" + k + ": {$id: '" + server.URL + "/s" + k + ".yaml', type: string, description: b" + k + "}\n")
		uses.WriteString("    U" + k + ": {$ref: '" + server.URL + "/s" + k + ".yaml'}\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", "bundle.yaml"), []byte(bundle.String()), 0o644))
	spec := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"+
		uses.String()+"    L: {$ref: 'lib/bundle.yaml#/s0'}\n"), 0o644))
	opts := engine.RunOptions{CompilerOptions: map[string]string{
		"allow-external-refs": "true", "max-external-documents": "1",
	}}
	eng, counter := cachedEngine(t, newMemoryCache(), "b1")

	first, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	require.False(t, first.Failed, "%v", first.Diagnostics)
	again, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)

	assert.Zero(t, hits.Load(), "the server saw no request, in either run")
	assert.EqualValues(t, 1, counter.compiles.Load(), "the entry was sound: the second run was answered from it")
	assertSameRun(t, first, again)
}
