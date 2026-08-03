package openapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// The compiler contract is that compilers.Source is the whole input:
// "Compilers perform no file I/O; the caller loads bytes so compilation stays
// pure and reentrant". A $ref naming another document is the one place that can
// be broken from inside a spec, so these tests observe the I/O rather than
// reading the code for it — a listening socket that counts requests, and a file
// whose presence must not change the answer.
//
// Each has a control in the same test: the same input with AllowExternalRefs
// set, where the I/O does happen. Without it, an assertion that nothing was read
// would also pass on a spec whose $ref was never attempted at all.

// externalDoc is a well-formed document for an external $ref to name.
const externalDoc = `openapi: 3.1.0
info: {title: External, version: "1"}
paths: {}
components:
  schemas:
    Thing: {type: object, properties: {name: {type: string}}}
`

// compileSpec runs the public entry point over one in-memory spec.
func compileWithOptions(t *testing.T, path, spec string, opts openapi.Options) []ir.Diagnostic {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(spec)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return diags
}

// TestCompile_ExternalHTTPRefIsNotFetchedByDefault holds the SSRF half: a $ref
// naming a URL must not put a request on the wire. The server counts what
// reaches it, so this observes the network rather than inferring it from the
// diagnostics — a compile that fetched and then reported the ref unresolved
// anyway would satisfy a diagnostics-only assertion.
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
	diags := compileWithOptions(t, "spec.yaml", spec, openapi.Options{})
	assert.Zero(t, hits.Load(), "compiling a spec must not make an outbound request")
	assert.True(t, hasErrorRef(diags), "and the ref it declined to follow is reported")

	// The control: the same spec with the opt-in does reach the server, so the
	// assertion above is about the default and not about an unreachable URL.
	compileWithOptions(t, "spec.yaml", spec, openapi.Options{AllowExternalRefs: true})
	assert.Positive(t, hits.Load(), "AllowExternalRefs is what puts the request on the wire")
}

// TestCompile_ExternalFileRefIsNotReadByDefault holds the arbitrary-file-read
// and determinism half: the same bytes must compile the same way whether or not
// the named file is sitting next to them.
//
// Both halves are the same differential, run under the two settings, because
// whether a file was opened is only observable in whether its presence changed
// the answer. Under the default it must not; under the opt-in it must, which is
// what keeps the first half from passing on a $ref that was never attempted.
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
	absentDefault := compileWithOptions(t, specPath, spec, openapi.Options{})
	absentAllowed := compileWithOptions(t, specPath, spec, openapi.Options{AllowExternalRefs: true})

	require.NoError(t, os.WriteFile(filepath.Join(dir, "external.yaml"), []byte(externalDoc), 0o600))
	presentDefault := compileWithOptions(t, specPath, spec, openapi.Options{})
	presentAllowed := compileWithOptions(t, specPath, spec, openapi.Options{AllowExternalRefs: true})

	assert.Equal(t, absentDefault, presentDefault,
		"a file the compiler never opens cannot change what the compiler reports")
	assert.True(t, hasErrorRef(presentDefault), "the ref it declined to follow is reported")

	assert.NotEqual(t, absentAllowed, presentAllowed,
		"AllowExternalRefs is what consults the filesystem: with it set, the same bytes "+
			"report differently depending on whether the named file is there")
}

// TestCompile_ExternalRefRefusalIsReportedOncePerDistinctFailure pins the shape
// of the refusal. The resolver returns one joined error over every reference it
// could not follow; rendering that join whole put four failures in the message
// field of one diagnostic, which read as the same sentence stuttered four times.
func TestCompile_ExternalRefRefusalIsReportedOncePerDistinctFailure(t *testing.T) {
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
	diags := compileWithOptions(t, "spec.yaml", spec, openapi.Options{})
	require.True(t, hasErrorRef(diags), "four refused refs are reported")

	for _, d := range diags {
		assert.NotContains(t, d.Message, "\n",
			"one diagnostic carries one message, not a joined list: %+v", d)
	}
}
