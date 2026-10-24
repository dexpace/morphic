package e2e_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// goldenPetstore is the larger real-ish spec seeded alongside the conformance
// corpus, addressed relative to this package.
const goldenPetstore = "../../../testdata/golden/openapi/petstore.yaml"

// amplificationBombFixture is a document whose YAML aliases expand it to many
// times its own size, which no alias budget admits.
const amplificationBombFixture = "../../../testdata/openapi/amplification_alias_bomb.yaml"

// reproducerDir is where the cycle fixtures live. readReproducer,
// TestCycleReproducers_EveryFixtureIsExercised and FuzzCycleDetector's seeds
// all derive their paths from it, so none of them can end up looking somewhere
// the others are not — which matters most for the seed loader, since it
// ignores a read error and a wrong path there would cost the fuzzer its seeds
// in silence.
const reproducerDir = "../../../testdata/openapi"

// corpusRoot is every committed spec directory, addressed relative to this
// package.
const corpusRoot = "../../../testdata"

// parseFull runs the whole public compiler pipeline over src. That reach back
// through openapi is also why openapitest cannot hold it — see that package's
// doc comment.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// compileAnnotationSpec compiles one in-memory annotation-retention spec.
// Every spec in this suite is well-formed by construction, so compilation is
// required to succeed with no Go error; the caller checks diagnostics
// separately, against what its own cell expects.
func compileAnnotationSpec(t *testing.T, name, spec string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// compileBesideExpectingErrors compiles root, written beside ext, with
// external references allowed, and returns the diagnostics instead of
// requiring none: compileBeside's twin comparisons need a clean compile, and a
// refused external document needs the diagnostic itself.
func compileBesideExpectingErrors(t *testing.T, ext, root string) []ir.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(ext), 0o600))
	_, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err, "a refused external document is a diagnostic, not a Go error")
	return diags
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

// readReproducer reads the named fixture from reproducerDir.
func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reproducerDir, file+".yaml"))
	require.NoError(t, err)
	return data
}

// reversed is src with every mapping's entries in reverse order.
func reversed(t *testing.T, src string) []byte {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	stack := []*yaml.Node{&root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == yaml.MappingNode {
			pairs := slices.Collect(slices.Chunk(n.Content, 2))
			slices.Reverse(pairs)
			n.Content = slices.Concat(pairs...)
		}
		stack = append(stack, n.Content...)
	}
	out, err := yaml.Marshal(&root)
	require.NoError(t, err)
	return out
}

// corpusSpecs lists every committed spec under testdata, as harness.SpecFiles
// lists a directory.
func corpusSpecs(t *testing.T) []string {
	t.Helper()
	out, err := harness.SpecFiles(t.Context(), corpusRoot)
	require.NoError(t, err)
	require.NotEmpty(t, out, "corpus sweep found no specs")
	return out
}

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner"). It stays a test-file
// copy for the reason openapitest's doc comment gives.
func componentID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[componentID(name)]
}

// namedID is the stable TypeID of a components-named schema.
func namedID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// allOperations flattens every operation across a document's service groups.
func allOperations(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	var walk func(gs []ir.OperationGroup)
	walk = func(gs []ir.OperationGroup) {
		for _, g := range gs {
			out = append(out, g.Operations...)
			walk(g.Groups)
		}
	}
	for _, svc := range doc.Services {
		walk(svc.Groups)
	}
	return out
}

// opByName finds an operation by its source operationId.
func opByName(doc *ir.Document, source string) (ir.Operation, bool) {
	for _, op := range allOperations(doc) {
		if op.Name.Source == source {
			return op, true
		}
	}
	return ir.Operation{}, false
}

// operationAt finds the operation bound to one HTTP method and URI template.
// Operations reached through a shared path item have no distinguishing name of
// their own, so their binding is what tells them apart. A webhook binding has
// no URI template, so no webhook is found.
func operationAt(t *testing.T, doc *ir.Document, method, uri string) ir.Operation {
	t.Helper()
	for _, op := range allOperations(doc) {
		for _, hb := range op.Bindings.HTTP {
			if hb.Method == method && hb.URITemplate == uri {
				return op
			}
		}
	}
	t.Fatalf("no operation bound to %s %s", method, uri)
	return ir.Operation{}
}

// operationsNamed returns every operation whose source operationId is name, in
// any group of any service, in the order the groups are walked.
func operationsNamed(doc *ir.Document, name string) []ir.Operation {
	groups := make([]ir.OperationGroup, 0, len(doc.Services))
	for _, svc := range doc.Services {
		groups = append(groups, svc.Groups...)
	}
	var ops []ir.Operation
	for len(groups) > 0 {
		g := groups[0]
		groups = append(groups[1:], g.Groups...)
		for _, op := range g.Operations {
			if op.Name.Source == name {
				ops = append(ops, op)
			}
		}
	}
	return ops
}

// paramByName returns the operation's logical parameter with the given source
// name.
func paramByName(op ir.Operation, name string) (ir.Parameter, bool) {
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p, true
		}
	}
	return ir.Parameter{}, false
}

// propByWire returns the property of m with the given wire name.
func propByWire(m *ir.Model, wire string) (ir.Property, bool) {
	for _, p := range m.Properties {
		if p.WireName == wire {
			return p, true
		}
	}
	return ir.Property{}, false
}

// headerByWire returns the response header with the given wire name.
func headerByWire(headers []ir.Property, wire string) (ir.Property, bool) {
	for _, h := range headers {
		if h.WireName == wire {
			return h, true
		}
	}
	return ir.Property{}, false
}

// firstContent returns an operation's single success-response content.
func firstContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	return op.Responses[0].Payload.Contents[0]
}

// requestContent returns an operation's single request-body content.
func requestContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	return op.Request.Contents[0]
}

// withoutVersion encodes doc with what records the declared version set aside:
// its source entries, which name the format and hash the bytes, and its
// diagnostics, which the test compares apart.
func withoutVersion(t *testing.T, doc *ir.Document) string {
	t.Helper()
	stripped := *doc
	stripped.Sources = nil
	stripped.Diagnostics = nil
	out, err := json.Marshal(&stripped, json.Deterministic(true))
	require.NoError(t, err)
	return string(out)
}

// unmodeledSite is one Unmodeled entry paired with the walk path of the map
// holding it.
type unmodeledSite struct {
	key   string
	path  string
	entry ir.UnmodeledEntry
}

// unmodeledSites returns every Unmodeled entry the document holds. It walks the
// value graph rather than naming carriers so that a test asserting "this is kept
// somewhere sensible" cannot pass by looking only where it expected to.
func unmodeledSites(doc *ir.Document) []unmodeledSite {
	unmodeledType := reflect.TypeFor[ir.Unmodeled]()
	var out []unmodeledSite
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() != unmodeledType || !v.CanInterface() {
			return true
		}
		u, ok := reflect.TypeAssert[ir.Unmodeled](v)
		if !ok {
			return true
		}
		for key, entry := range u {
			out = append(out, unmodeledSite{key: key, path: path, entry: entry})
		}
		return true
	})
	return out
}

// findUnmodeled returns the site holding key with the given JSON value. The
// value is part of the match because one key spelling occurs at many carriers —
// "openapi:x-mark" is written on a dozen objects in this fixture — so matching
// on the key alone would find a different object's entry and call it a pass.
func findUnmodeled(sites []unmodeledSite, key, wantJSON string) (unmodeledSite, bool) {
	for _, site := range sites {
		if site.key == key && string(site.entry.Value) == wantJSON {
			return site, true
		}
	}
	return unmodeledSite{}, false
}

// unmodeledEntry returns the Unmodeled entry at key, failing when absent.
func unmodeledEntry(t *testing.T, p ir.Unmodeled, key string) ir.UnmodeledEntry {
	t.Helper()
	entry, ok := p[key]
	require.True(t, ok, "%s kept verbatim; got %v", key, p)
	return entry
}

// assertHasErrorCode requires diags to carry an error-severity diagnostic with
// the given code.
func assertHasErrorCode(t *testing.T, diags []ir.Diagnostic, code string) {
	t.Helper()
	openapitest.AssertHasCode(t, diags, code, ir.SeverityError)
}

// assertNoErrorDiags fails when any diagnostic has error severity.
func assertNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	assertOnlyErrorDiags(t, diags, nil)
}

// diagsAt returns the severities of the diagnostics coded code at pointer.
func diagsAt(diags []ir.Diagnostic, code, pointer string) []ir.Severity {
	var out []ir.Severity
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Severity)
		}
	}
	return out
}

// hasErrorRef reports whether the diagnostics carry an error-severity
// unresolved-ref finding — the signal that an offending entry was dropped.
func hasErrorRef(diags []ir.Diagnostic) bool {
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Severity == ir.SeverityError && d.Code == "openapi/unresolved-ref"
	})
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

// compileClean compiles src with opts and requires no error diagnostic.
func compileClean(t *testing.T, src string, opts openapi.Options) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	return doc
}

// assertOnlyErrorDiags fails when a diagnostic has error severity and a code
// outside want, or when a code of want draws no error.
func assertOnlyErrorDiags(t *testing.T, diags []ir.Diagnostic, want []string) {
	t.Helper()
	drawn := map[string]bool{}
	for _, d := range diags {
		if d.Severity != ir.SeverityError {
			continue
		}
		drawn[d.Code] = true
		assert.Contains(t, want, d.Code, "unexpected error diagnostic: %+v", d)
	}
	for _, code := range want {
		assert.True(t, drawn[code], "the spec is listed as invalid but draws no %s", code)
	}
}
