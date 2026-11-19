// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// servedDocs is an HTTP server on the loopback address that serves each
// document of docs at its path, and notes every request line it is sent.
type servedDocs struct {
	*httptest.Server

	mu   sync.Mutex
	docs map[string]string
	hits []string
}

// serveDocs starts a server of docs, keyed by the absolute path each is
// served at, such as "/c.yaml". It is closed when the test ends.
func serveDocs(t *testing.T, docs map[string]string) *servedDocs {
	t.Helper()
	servedDocsValue := &servedDocs{docs: docs}
	servedDocsValue.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servedDocsValue.mu.Lock()
		servedDocsValue.hits = append(servedDocsValue.hits, r.RequestURI)
		servedDocsValue.mu.Unlock()
		body, ok := servedDocsValue.docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(servedDocsValue.Close)
	return servedDocsValue
}

// requests lists the request lines the server was sent, in order.
func (s *servedDocs) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hits...)
}

// hostMapReader answers a request for a URL under one of routes' prefixes
// from the URL that prefix maps to, so a document can be served at a host
// name no test may reach. Every other request is answered by inner.
type hostMapReader struct {
	inner  compilers.DocumentReader
	routes map[string]string
}

// ReadDocument rewrites req.Location through the first route that matches and
// reads it through inner.
func (r hostMapReader) ReadDocument(ctx context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	for from, to := range r.routes {
		if rest, ok := strings.CutPrefix(req.Location, from); ok {
			req.Location = to + rest
			break
		}
	}
	return r.inner.ReadDocument(ctx, req)
}

// compileWithReader compiles src, named path, reading what it names through
// rd with external references allowed or not, and returns the document and
// every diagnostic.
func compileWithReader(t *testing.T, path, src string, opts openapi.Options, rd compilers.DocumentReader) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(src)}},
		compilers.Options{Reader: rd, FormatOptions: opts})
	require.NoError(t, err, "a spec problem is a diagnostic, not a Go error")
	require.NotNil(t, doc)
	return doc, diags
}

// useTarget is the type the component schema name is a use of, as the node a
// component owns over it names it: the Base of the Model over an object, or of
// the Scalar alias over anything else.
func useTarget(t *testing.T, doc *ir.Document, name string) ir.TypeID {
	t.Helper()
	return useOf(t, typeByName(doc, name), name)
}

// useOf is the type td, the node a position owns over what its $ref names, is
// a use of: the Base of the Model over an object or of the Scalar alias.
func useOf(t *testing.T, td ir.TypeDef, name string) ir.TypeID {
	t.Helper()
	switch n := td.(type) {
	case *ir.Model:
		require.NotNil(t, n.Base, "%s composes the object it names", name)
		require.Empty(t, n.Properties, "%s declares no member of its own", name)
		return n.Base.Target
	case *ir.Scalar:
		require.NotNil(t, n.Base, "%s is an alias of the type it names", name)
		return n.Base.Target
	default:
		require.Failf(t, "no use", "%s is %T, no use of the type it names", name, n)
		return ""
	}
}

// sourcePaths lists the path of each Document.Sources entry, in order.
func sourcePaths(doc *ir.Document) []string {
	out := make([]string, 0, len(doc.Sources))
	for _, sourceInfo := range doc.Sources {
		out = append(out, sourceInfo.Path)
	}
	return out
}

// labelled is the ID of the type declared at pointer in the document labelled
// label.
func labelled(label, pointer string) ir.TypeID {
	return ids.In(label).ForPointer(jsontext.Pointer(pointer))
}

// failingReader fails every read it is asked for, so a compile that reads
// through it learns whether it reads at all.
type failingReader struct{}

// ReadDocument fails, naming the document asked for.
func (failingReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	return compilers.DocumentRead{}, errors.New("no read is expected, got " + req.Location)
}

// loopbackDisk reads files as reader.Disk does, and URLs only on the loopback
// address: an unconfined request for any other host fails here, so a compile
// whose mapping or spelling is wrong cannot reach the network from a test. A
// confined request is Disk's to refuse, which it does before any dial.
type loopbackDisk struct{}

// ReadDocument reads req through reader.Disk unless it names a host other than
// 127.0.0.1.
func (loopbackDisk) ReadDocument(ctx context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	lower := strings.ToLower(req.Location)
	isURL := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
	if isURL && req.Within == "" && !strings.HasPrefix(strings.SplitN(lower, "://", 2)[1], "127.0.0.1:") {
		return compilers.DocumentRead{}, errors.New("a test reaches no host but the loopback, got " + req.Location)
	}
	return reader.Disk{}.ReadDocument(ctx, req)
}
