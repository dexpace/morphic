package openapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// The compiler contract is that it does I/O only through
// compilers.Options.Reader: with none, nothing is read, whatever else is set,
// and with one, every read is the reader's. A $ref naming another document is
// the one place a spec can reach for I/O, so these tests observe it rather
// than reading the code for it — a listening socket that counts requests, a
// file whose presence must not change the answer, and a reader that serves
// from memory what no disk holds.
//
// Each has a control in the same test: the same input with a Reader, where the
// I/O does happen. Without it, an assertion that nothing was read would also
// pass on a spec whose $ref was never attempted.

// externalDoc is a well-formed document for an external $ref to name.
const externalDoc = `openapi: 3.1.0
info: {title: External, version: "1"}
paths: {}
components:
  schemas:
    Thing: {type: object, properties: {name: {type: string}}}
`

// compileDiags runs the public entry point over one in-memory spec.
func compileDiags(t *testing.T, path, spec string, opts compilers.Options) []ir.Diagnostic {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(spec)}}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return diags
}

// withExternalRefs returns the options of a compile that allows external
// references, so a reader supplied beside them is unconfined.
func withExternalRefs(r compilers.DocumentReader) compilers.Options {
	return compilers.Options{Reader: r, FormatOptions: openapi.Options{AllowExternalRefs: true}}
}

// TestCompile_ExternalHTTPRefIsNotFetchedByDefault holds the SSRF half: with
// no Reader a $ref naming a URL must not put a request on the wire, whether or
// not external references are allowed, since allowing them only widens what a
// reader may read. The server counts what reaches it, so this observes the
// network rather than inferring it from the diagnostics — a compile that
// fetched and then reported the ref unresolved anyway would satisfy a
// diagnostics-only assertion.
func TestCompile_ExternalHTTPRefIsNotFetchedByDefault(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(externalDoc))
	}))
	t.Cleanup(srv.Close)

	spec := `openapi: 3.1.0
info: {title: Main, version: "1"}
paths: {}
components:
  schemas:
    Ref: {$ref: "` + srv.URL + `/external.yaml#/components/schemas/Thing"}
`
	for _, allow := range []bool{false, true} {
		diags := compileDiags(t, "spec.yaml", spec,
			compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: allow}})
		assert.Zero(t, hits.Load(), "compiling a spec with no reader must not make an outbound request (allow %t)", allow)
		assert.True(t, hasErrorRef(diags), "and the ref it declined to follow is reported (allow %t)", allow)
	}

	// The control: the same spec with a reader and the opt-in does reach the
	// server, so the assertions above are about the absence of a reader and not
	// about an unreachable URL.
	compileDiags(t, "spec.yaml", spec, withExternalRefs(reader.Disk{}))
	assert.Positive(t, hits.Load(), "a reader is what puts the request on the wire")
}

// TestCompile_ExternalFileRefIsNotReadByDefault holds the arbitrary-file-read
// and determinism half: with no Reader the same bytes must compile the same
// way whether or not the named file is sitting next to them, however external
// references are set.
//
// Both halves are the same differential, run under the two settings, because
// whether a file was opened is only observable in whether its presence changed
// the answer. With no reader it must not; with one it must, which is what
// keeps the first half from passing on a $ref that was never attempted.
func TestCompile_ExternalFileRefIsNotReadByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	const spec = `openapi: 3.1.0
info: {title: Main, version: "1"}
paths: {}
components:
  schemas:
    Ref: {$ref: "./external.yaml#/components/schemas/Thing"}
`
	noReader := func(allow bool) compilers.Options {
		return compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: allow}}
	}
	absentDefault := compileDiags(t, specPath, spec, noReader(false))
	absentAllowed := compileDiags(t, specPath, spec, noReader(true))
	absentRead := compileDiags(t, specPath, spec, withExternalRefs(reader.Disk{}))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "external.yaml"), []byte(externalDoc), 0o600))
	presentDefault := compileDiags(t, specPath, spec, noReader(false))
	presentAllowed := compileDiags(t, specPath, spec, noReader(true))
	presentRead := compileDiags(t, specPath, spec, withExternalRefs(reader.Disk{}))

	assert.Equal(t, absentDefault, presentDefault,
		"a file the compiler never opens cannot change what the compiler reports")
	assert.Equal(t, absentAllowed, presentAllowed,
		"allowing external references opens nothing when no reader was supplied")
	assert.True(t, hasErrorRef(presentDefault), "the ref it declined to follow is reported")
	assert.True(t, hasErrorRef(presentAllowed), "with external references allowed too")

	assert.NotEqual(t, absentRead, presentRead,
		"a reader is what consults the filesystem: with one, the same bytes "+
			"report differently depending on whether the named file is there")
}

// TestCompile_ReferencesAreReadOnlyThroughTheReader pins the other half of the
// contract: with a Reader every document a $ref names comes from it. The only
// copy of the file lives in the reader's memory, at a path no disk holds, and
// the ref resolves from it, so the compile read nothing itself; the reader was
// asked for exactly the document the ref names.
func TestCompile_ReferencesAreReadOnlyThroughTheReader(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nowhere")
	require.NoDirExists(t, dir, "sanity: no disk holds the document")
	specPath := filepath.Join(dir, "spec.yaml")
	const spec = `openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: "./common.yaml#/components/responses/R"}
`
	const common = "components:\n  responses:\n    R: {description: from the reader}\n"
	memory, err := reader.NewMemory([]compilers.Source{{Path: filepath.Join(dir, "common.yaml"), Data: []byte(common)}})
	require.NoError(t, err)
	recorded := &recordingReader{inner: memory}

	diags := compileDiags(t, specPath, spec, compilers.Options{Reader: recorded})

	assert.False(t, hasErrorRef(diags), "the ref resolves from what the reader holds: %+v", diags)
	requested := recorded.locations()
	assert.Equal(t, []string{filepath.Join(dir, "common.yaml")}, requested, "the reader was asked for the one document named")
}

// TestCompile_ExternalRefRefusalIsReportedAtEachRef pins the shape of the
// refusal: four external $refs the compiler refuses to follow are four reports,
// each one diagnostic at the $ref that produced it (GitHub #385), not one joined
// error rendered as the same sentence stuttered four times.
func TestCompile_ExternalRefRefusalIsReportedAtEachRef(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: "a.yaml#/components/parameters/P"}
      responses:
        "200": {$ref: "b.yaml#/components/responses/R"}
components:
  schemas:
    A: {$ref: "c.yaml#/components/schemas/X"}
    B: {$ref: "d.yaml#/components/schemas/Y"}
`
	diags := compileDiags(t, "spec.yaml", spec, compilers.Options{})
	require.True(t, hasErrorRef(diags), "four refused refs are reported")

	var pointers []string
	for _, d := range diags {
		if d.Code != diag.UnresolvedRef {
			continue
		}
		assert.NotContains(t, d.Message, "\n",
			"one diagnostic carries one message, not a joined list: %+v", d)
		pointers = append(pointers, string(d.Provenance.Pointer))
	}
	slices.Sort(pointers)
	assert.Equal(t, []string{
		"/components/schemas/A",
		"/components/schemas/B",
		"/paths/~1a/get/parameters/0",
		"/paths/~1a/get/responses/200",
	}, pointers, "each refusal is its own finding, at its own $ref")
}
