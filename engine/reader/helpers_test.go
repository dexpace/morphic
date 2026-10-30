// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile writes content to the slash-separated name under dir, making the
// directories on the way, and returns the file's path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	return full
}

// symlink makes link a symbolic link to target, skipping the test where the
// system will not make one.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o750))
	err := os.Symlink(target, link)
	if err != nil && runtime.GOOS == "windows" {
		t.Skipf("a symbolic link cannot be made here: %v", err)
	}
	require.NoError(t, err)
}

// fileURI returns the file URI naming the absolute path p.
func fileURI(p string) string {
	slashed := filepath.ToSlash(p)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// expander returns the function that reads {root} as root, {rooturi} as its
// file URI, {localuri} as that URI with its authority spelled localhost,
// {ROOTURI} as that URI with its scheme in capitals, {srv} as the test
// server's URL and {SRV} as that URL with its scheme in capitals.
func expander(root, srv string) func(string) string {
	rootURI := fileURI(root)
	return strings.NewReplacer(
		"{root}", root,
		"{rooturi}", rootURI,
		"{localuri}", strings.Replace(rootURI, "file://", "file://localhost", 1),
		"{ROOTURI}", "FILE"+strings.TrimPrefix(rootURI, "file"),
		"{srv}", srv,
		"{SRV}", "HTTP"+strings.TrimPrefix(srv, "http"),
	).Replace
}

// countingServer starts an httptest.Server for handler, closed when the test
// ends, and returns it with the count of requests it has served.
func countingServer(t *testing.T, handler http.Handler) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// serveBody returns a handler answering every request with status and body.
func serveBody(t *testing.T, status int, body string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, err := io.WriteString(w, body)
		assert.NoError(t, err)
	})
}

// roundTripFunc is an http.RoundTripper answering with a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// cancelled returns a context already cancelled.
func cancelled(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}
