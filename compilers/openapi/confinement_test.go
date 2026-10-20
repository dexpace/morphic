// This file is a package-level suite, not a per-source-file test: it pins the
// confinement a compile gives the reader it was handed, end to end through the
// public entry point, where the load phase's own tests hold its parts.
package openapi_test // external test package — exercises only the public API

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// recordingReader reads through inner and records each request it was handed.
type recordingReader struct {
	inner compilers.DocumentReader

	mu   sync.Mutex
	reqs []compilers.DocumentRequest
}

// ReadDocument records req, then answers it as inner does.
func (r *recordingReader) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.inner.ReadDocument(ctx, req)
}

// requests returns the requests recorded so far, in order.
func (r *recordingReader) requests() []compilers.DocumentRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]compilers.DocumentRequest(nil), r.reqs...)
}

// locations returns the document each recorded request named, in order.
func (r *recordingReader) locations() []string {
	reqs := r.requests()
	out := make([]string, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, req.Location)
	}
	return out
}

// otherDoc holds the one response and parameter a confinement test's $refs
// name, each told apart from anything the source declares by its description.
const otherDoc = `components:
  responses:
    R: {description: from the other file}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`

// refsInto returns a source whose GET /a takes the parameter P and answers 200
// with the response R, each a $ref into the document at doc.
func refsInto(doc string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: "` + doc + `#/components/parameters/P"}
      responses:
        "200": {$ref: "` + doc + `#/components/responses/R"}
`
}

// layout writes files, named by their slash-separated path under root, creating
// the directories they sit in.
func layout(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

// compileOnDisk compiles source, read from path, through reader.Disk with
// external references allowed or not.
func compileOnDisk(t *testing.T, path, source string, allow bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(source)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// unresolvedRefs returns the unresolved-ref findings among diags.
func unresolvedRefs(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			out = append(out, d)
		}
	}
	return out
}

// requireReadFrom requires that GET /a took its parameter and its response
// from the other document, and reported no unresolved reference.
func requireReadFrom(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	require.Empty(t, unresolvedRefs(diags), "no reference was refused")
	op := operationAt(t, doc, "GET", "/a")
	require.Len(t, op.Params, 1)
	assert.Equal(t, "p", op.Params[0].Name.Source, "the parameter is the other file's")
	require.Len(t, op.Responses, 1)
	assert.Equal(t, "from the other file", op.Responses[0].Docs.Description, "the response is the other file's")
}

// requireRefused requires that both references into the other document were
// reported unresolved, with reason in each message, and that nothing of the
// document reached the operation.
func requireRefused(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, reason string) {
	t.Helper()
	refused := unresolvedRefs(diags)
	require.Len(t, refused, 2, "the parameter and the response: %+v", diags)
	for _, d := range refused {
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Contains(t, d.Message, reason, "at %s", d.Provenance.Pointer)
	}
	op := operationAt(t, doc, "GET", "/a")
	assert.Empty(t, op.Params, "the refused parameter is absent")
	for _, r := range op.Responses {
		assert.NotEqual(t, "from the other file", r.Docs.Description, "the refused document was not read")
	}
}

// TestCompile_ASiblingRefResolvesWithinTheSourcesDirectory pins what the
// default permits: with a reader and external references off, a $ref to a
// document in the source's own directory, or below it, is read and resolves.
func TestCompile_ASiblingRefResolvesWithinTheSourcesDirectory(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"the same directory": "./common.yaml",
		"a subdirectory":     "./sub/deeper/common.yaml",
		"a detour inside":    "./sub/../common.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			layout(t, dir, map[string]string{"common.yaml": otherDoc, "sub/deeper/common.yaml": otherDoc})

			got, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), refsInto(doc), false)

			requireReadFrom(t, got, diags)
		})
	}
}

// TestCompile_ARefOutsideTheSourcesDirectoryIsRefused pins the default
// confinement: a $ref whose document lies beside or above the source's
// directory is reported unresolved as lying outside it, its file is not read,
// and the position lowers without it.
func TestCompile_ARefOutsideTheSourcesDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"a parent directory":  "../outside.yaml",
		"a sibling directory": "../other/outside.yaml",
		"a detour out":        "./sub/../../outside.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			layout(t, base, map[string]string{"outside.yaml": otherDoc, "other/outside.yaml": otherDoc, "src/spec.yaml": ""})

			got, diags := compileOnDisk(t, filepath.Join(base, "src", "spec.yaml"), refsInto(doc), false)

			requireRefused(t, got, diags, "lies outside")
		})
	}
}

// TestCompile_ARefOutsideTheSourcesDirectoryResolvesWhenAllowed is the control
// for the refusal above, on the same layout: with external references allowed
// the reader is unconfined and the same $ref resolves. Without it the refusal
// could be a document that was never there.
func TestCompile_ARefOutsideTheSourcesDirectoryResolvesWhenAllowed(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	layout(t, base, map[string]string{"outside.yaml": otherDoc, "src/spec.yaml": ""})

	got, diags := compileOnDisk(t, filepath.Join(base, "src", "spec.yaml"), refsInto("../outside.yaml"), true)

	requireReadFrom(t, got, diags)
}

// TestCompile_ASymlinkOutOfTheSourcesDirectoryIsRefused pins that confinement
// holds against a link, which a lexical check of the path would pass: a file
// inside the source's directory that points outside it is refused, and read
// once external references are allowed, which shows the link itself is sound.
func TestCompile_ASymlinkOutOfTheSourcesDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("creating a symbolic link needs a privilege most Windows runs lack")
	}
	base := t.TempDir()
	layout(t, base, map[string]string{"outside.yaml": otherDoc, "src/spec.yaml": ""})
	if err := os.Symlink(filepath.Join(base, "outside.yaml"), filepath.Join(base, "src", "link.yaml")); err != nil {
		t.Skipf("a link cannot be made here: %v", err)
	}
	path := filepath.Join(base, "src", "spec.yaml")

	refusedDoc, refusedDiags := compileOnDisk(t, path, refsInto("./link.yaml"), false)
	allowedDoc, allowedDiags := compileOnDisk(t, path, refsInto("./link.yaml"), true)

	refused := unresolvedRefs(refusedDiags)
	require.Len(t, refused, 2, "the parameter and the response: %+v", refusedDiags)
	assert.Empty(t, operationAt(t, refusedDoc, "GET", "/a").Params, "nothing of the linked file was read")
	requireReadFrom(t, allowedDoc, allowedDiags)
}

// TestCompile_ARelativeSourcePathIsConfinedToItsDirectory pins the confinement
// for the path a command line gives most often, one relative to the working
// directory: the directory is the path's own, so a sibling resolves and a
// document beside that directory is outside it.
func TestCompile_ARelativeSourcePathIsConfinedToItsDirectory(t *testing.T) {
	base := t.TempDir()
	layout(t, base, map[string]string{"outside.yaml": otherDoc, "src/common.yaml": otherDoc})
	t.Chdir(base)
	path := filepath.Join("src", "spec.yaml")

	inside, insideDiags := compileOnDisk(t, path, refsInto("./common.yaml"), false)
	outside, outsideDiags := compileOnDisk(t, path, refsInto("../outside.yaml"), false)

	requireReadFrom(t, inside, insideDiags)
	requireRefused(t, outside, outsideDiags, "lies outside")
}

// TestCompile_TheReaderIsConfinedToTheSourcesDirectoryUnlessAllowed pins what
// the compile asks the reader for, which is all a reader sees of the option:
// the directory of the source's path with external references off, whatever
// form the path takes, none with them on, and the byte budget either way. It
// is observed on a reader that records its requests.
func TestCompile_TheReaderIsConfinedToTheSourcesDirectoryUnlessAllowed(t *testing.T) {
	t.Parallel()
	const source = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: "./other.yaml#/components/responses/R"}
`
	for _, p := range []struct{ name, path string }{
		{"an absolute path", filepath.Join(filepath.FromSlash("/srv/specs"), "spec.yaml")},
		{"a nested relative path", filepath.Join("api", "spec.yaml")},
		{"a bare file name", "spec.yaml"},
	} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, external references %t", p.name, allow), func(t *testing.T) {
				t.Parallel()
				path := p.path
				nothing, err := reader.NewMemory(nil)
				require.NoError(t, err)
				recorded := &recordingReader{inner: nothing}
				wantWithin := filepath.Dir(path)
				if allow {
					wantWithin = ""
				}

				_, _, err = openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(source)}},
					compilers.Options{Reader: recorded, FormatOptions: openapi.Options{AllowExternalRefs: allow}})

				require.NoError(t, err)
				want := []compilers.DocumentRequest{{
					Location: filepath.Join(filepath.Dir(path), "other.yaml"),
					Within:   wantWithin,
					Limit:    openapi.DefaultMaxSourceBytes,
				}}
				assert.Empty(t, cmp.Diff(want, recorded.requests()))
			})
		}
	}
}

// TestCompile_AURLRefIsNotFetchedWhileReadsAreConfined pins that the default
// confinement holds the network too: a URL lies inside no directory, so with a
// reader and external references off a $ref naming one is refused as on the
// network and no request reaches the server. The control allows them, and the same
// spec fetches.
func TestCompile_AURLRefIsNotFetchedWhileReadsAreConfined(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(otherDoc))
	}))
	t.Cleanup(srv.Close)
	source := refsInto(srv.URL + "/common.yaml")
	path := filepath.Join(t.TempDir(), "spec.yaml")

	refusedDoc, refusedDiags := compileOnDisk(t, path, source, false)

	assert.Zero(t, hits.Load(), "a confined compile makes no outbound request")
	requireRefused(t, refusedDoc, refusedDiags, "it is on the network")

	allowedDoc, allowedDiags := compileOnDisk(t, path, source, true)

	assert.Positive(t, hits.Load(), "the control reaches the server")
	requireReadFrom(t, allowedDoc, allowedDiags)
}
