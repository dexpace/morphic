// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// countingServer is a loopback server counting the requests it is asked.
func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestCompile_ASourceAtAURLReachesNoLocalAddressThroughTheDisk pins the dial
// bound through the compile and the reader every caller uses: a source
// located at a public URL names a loopback server in every spelling an
// address takes, directly and through a proxy, and each reference is refused
// with the reach's reason, with nothing asked of the server or the proxy.
func TestCompile_ASourceAtAURLReachesNoLocalAddressThroughTheDisk(t *testing.T) {
	t.Parallel()
	srv, hits := countingServer(t, "X: {type: string}\n")
	proxy, proxied := countingServer(t, "X: {type: string}\n")
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1")
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	viaProxy := reader.Disk{Client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}}
	for name, tc := range map[string]struct {
		docs  compilers.DocumentReader
		hosts []string
	}{
		"dialled": {reader.Disk{}, []string{
			"127.0.0.1" + port, "0.0.0.0" + port, "[::]" + port, "[::ffff:127.0.0.1]" + port,
			"[::1]" + port, "user:secret@127.0.0.1" + port, "127.0.0.1:1",
		}},
		"through a proxy": {viaProxy, []string{
			"10.0.0.5", "127.0.0.1" + port, "0177.0.0.1", "0x7f.1", "2130706433", "127.1",
			"localhost.", "LOCALHOST..", "ｍocalhost", "127。0。0。1",
		}},
	} {
		var schemas strings.Builder
		for i, host := range tc.hosts {
			schemas.WriteString("    S" + string(rune('A'+i)) + ": {$ref: 'http://" + host + "/x.yaml#/X'}\n")
		}
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "spec.yaml", Location: "https://pub.example/spec.yaml",
				Data: []byte(dynHead + schemas.String())}},
			compilers.Options{Reader: tc.docs, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		errs := byCode(diags, "openapi/unresolved-ref")
		require.Len(t, errs, len(tc.hosts), "%s: %v", name, diags)
		for _, diagnostic := range errs {
			assert.Equal(t, ir.SeverityError, diagnostic.Severity)
			assert.Contains(t, diagnostic.Message, "refused: ", "%s: %s", name, diagnostic.Message)
			t.Logf("%s: %s %s", name, diagnostic.Provenance.Pointer, diagnostic.Message)
		}
	}
	assert.Zero(t, hits.Load(), "the server was asked nothing")
	assert.Zero(t, proxied.Load(), "the proxy was asked nothing")
}
