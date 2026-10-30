// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// goldenPetstore is the golden petstore spec, addressed relative to this
// package.
const goldenPetstore = "../../testdata/golden/openapi/petstore.yaml"

// parsePetstore compiles the golden petstore spec through the public pipeline.
// Reading the on-disk spec rather than a hand-copied literal keeps this test
// coupled to the same document the golden snapshot is compared against, so an
// edit to the fixture cannot leave this test asserting against a stale copy.
func parsePetstore(t *testing.T) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{{Path: "petstore.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

func TestCompile_EndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := parsePetstore(t)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
	assert.Equal(t, ir.IRVersion, doc.IRVersion)
	assert.Equal(t, "Petstore", doc.Name)
	assert.Equal(t, "1.0.0", doc.Version)
	require.NotNil(t, doc.Contact)
	assert.Equal(t, "API Team", doc.Contact.Name)
	require.NotNil(t, doc.License)
	assert.Equal(t, "MIT", doc.License.Name)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Sources, 1)
	assert.Len(t, doc.Sources[0].Hash, 64)

	// One server with a templated variable survived, and the service names it.
	require.Len(t, doc.Servers, 1)
	require.Equal(t, []ir.ServerID{"server/openapi/servers/0"}, doc.Services[0].Servers)
	server := doc.Servers["server/openapi/servers/0"]
	assert.Equal(t, "https://{env}.example.com/v1", server.URLTemplate)
	require.Len(t, server.Variables, 1)
	assert.Equal(t, "env", server.Variables[0].Name)
	assert.Equal(t, []string{"api", "staging"}, server.Variables[0].Enum)

	// Document-level security lowered to the service default (OR-of-ANDs).
	require.Len(t, doc.Services[0].Auth, 1)
	require.Len(t, doc.Services[0].Auth[0].Schemes, 1)
	assert.Equal(t, []string{"read:pets"}, doc.Services[0].Auth[0].Schemes[0].Scopes)

	// Auth scheme registry: the oauth2 scheme lowered with its implicit flow.
	require.Len(t, doc.Auth, 1)
	var oauth ir.AuthScheme
	for _, s := range doc.Auth {
		oauth = s
	}
	assert.Equal(t, ir.AuthKindOAuth2, oauth.Kind)
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "implicit", oauth.Flows[0].Kind)

	// Spot-checks over the type registry: the named schema is present under its
	// pointer ID, at least one anonymous type was hoisted, and the oneOf
	// survived as a Union node rather than being collapsed.
	var pet ir.TypeDef
	var sawAnon, sawUnion bool
	for _, td := range doc.Types {
		if td.Common().Name.Source == "Pet" {
			pet = td
		}
		if td.Common().Anonymous {
			sawAnon = true
		}
		if td.Kind() == ir.KindUnion {
			sawUnion = true
		}
	}
	require.NotNil(t, pet, "named schema Pet present in the type registry")
	assert.Equal(t, ir.KindModel, pet.Kind())
	assert.True(t, sawAnon, "the inline meta object was hoisted as an anonymous type")
	assert.True(t, sawUnion, "the oneOf survived as a Union node")
}

// TestCompile_RecordsSelfURI pins that a declared `$self` reaches the source's
// entry as written, at every minor, and is reported below 3.2 as a construct a
// later version added, as every 3.2 field is. A relative URI is not resolved
// against the source's path, and an empty or null `$self` records nothing,
// which is what declaring none records.
func TestCompile_RecordsSelfURI(t *testing.T) {
	t.Parallel()
	const uri = "https://example.com/api/openapi.yaml"
	tests := []struct {
		name    string
		version string
		self    string // the $self line, or "" for none
		want    string
	}{
		{"3.2 declares one", "3.2.0", "$self: " + uri, uri},
		{"3.1 declares one", "3.1.0", "$self: " + uri, uri},
		{"3.0 declares one", "3.0.3", "$self: " + uri, uri},
		{"a relative one stays relative", "3.2.0", "$self: ../api/openapi.yaml", "../api/openapi.yaml"},
		{"none declared", "3.2.0", "", ""},
		{"an empty one is none", "3.2.0", `$self: ""`, ""},
		{"a null one is none", "3.2.0", "$self: null", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: " + tt.version + "\n" + tt.self + "\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
			doc, diags, err := openapi.New().Compile(context.Background(),
				[]compilers.Source{{Path: "specs/self-uri.yaml", Data: []byte(spec)}}, compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc, "compile refused: %+v", diags)
			assert.False(t, ir.HasError(diags), "%+v", diags)
			require.Len(t, doc.Sources, 1)
			assert.Equal(t, tt.want, doc.Sources[0].SelfURI)
			assert.Equal(t, tt.self != "" && tt.version != "3.2.0",
				slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
					return d.Code == diag.LaterVersionConstruct && d.Provenance.Pointer == "/$self"
				}))
		})
	}
}

func TestCompile_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	c := openapi.New()
	require.NoError(t, reg.Register(c))
	assert.Equal(t, c.Formats(), reg.Formats())

	src := compilers.Source{Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n")}
	det, ok, err := reg.Detect(t.Context(), src, nil)
	require.NoError(t, err)
	require.True(t, ok, "the registry hands it the documents it reads")
	assert.Same(t, compilers.Compiler(c), det.Compiler)
}

func TestCompile_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	doc, _ := parsePetstore(t)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	if diff := cmp.Diff(doc, &back); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
	again, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again), "marshal must be deterministic")
}

func TestCompile_RejectsMultipleSources(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(t, err)
	_, _, err = openapi.New().Compile(context.Background(),
		[]compilers.Source{
			{Path: "a.yaml", Data: data},
			{Path: "b.yaml", Data: data},
		}, compilers.Options{})
	require.Error(t, err)
}

// TestCompile_AnUnreadableSourceIsADiagnostic pins where the complaint about a
// broken document comes from when nothing detected it: a caller compiling
// directly hands Compile bytes no parse has read. It has to stay a diagnostic:
// engine.Run turns a compiler's Go error into its own, and the CLI maps that to
// exit 2 — the code it uses for being invoked wrong — so a spec it read would
// be reported as a misuse of itself.
func TestCompile_AnUnreadableSourceIsADiagnostic(t *testing.T) {
	t.Parallel()
	src := "bad: [unterminated\n" + strings.Repeat("filler: x\n", 8000) + "openapi: 3.1.0\n"
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{{Path: "api.yaml", Data: []byte(src)}}, compilers.Options{})

	require.NoError(t, err, "a document that will not parse is a finding, not a failure of the compiler")
	assert.Nil(t, doc)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UndecodableSource, diags[0].Code)
}

func TestCompile_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	spec := "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	doc, diags, err := openapi.New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "unsupported version refuses to lower")
	assert.True(t, openapitest.HasDiag(diags, diag.UnsupportedVersion))
}

// TestCompile_UnmarshalError pins where a document that will not parse is
// reported. It is a finding about the source, not a failure of the compiler:
// engine.Run turns a compiler's Go error into one of its own, and the CLI reads
// that as having been invoked wrong rather than as a spec it could not read.
func TestCompile_UnmarshalError(t *testing.T) {
	t.Parallel()
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf("\t\t: : : not valid : yaml\n\x00")}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UndecodableSource, diags[0].Code)
	assert.Equal(t, openapi.RootSrcIndex, diags[0].Provenance.Source,
		"no document comes back, but SourceTable names the source a refusal indexes")
}

// TestCompile_KeyThatIsNotUTF8IsRefused pins what ids.Ptr relies on: a source with
// a mapping key that is not UTF-8 is refused before anything lowers, so no such
// key becomes a pointer token.
func TestCompile_KeyThatIsNotUTF8IsRefused(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n\xff: x\n"
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "an undecodable source refuses to lower; no types are interned")
	assert.True(t, openapitest.HasDiag(diags, diag.UndecodableSource))
}

// TestRun_RegistryRefusalsAreSurfaced covers the reporting of an entry
// compile.Types declined to hold.
//
// No spec can provoke one — an empty ID or a nil type definition is a compiler
// bug — so the refusal is forced directly. Without this the framework would
// decline garbage and say nothing, which hides the bug instead of the symptom:
// the node is simply absent and every reference to it dangles.
func TestRun_RegistryRefusalsAreSurfaced(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Register("", nil)

	_, diags, err := openapi.Run(t.Context(), lowering.Ctx{Doc: &soa.OpenAPI{}}, types)
	require.NoError(t, err)

	assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Code == compile.RegistryRefused && strings.Contains(d.Message, "register rejected: empty type id")
	}), "the compile reports the refusal Finish carries: %v", diags)
}

// TestWithoutRereported is a unit table over the ways a lowering diagnostic
// and the load phase's diagnostics can relate. Only one drops anything: the key
// withoutRereported dedupes on is the code and the whole Provenance, so another
// code at the same provenance survives, and so does the same code at another
// pointer, or at the same pointer in another source.
func TestWithoutRereported(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}
	elsewhere := ir.Provenance{Source: 0, Pointer: "/components/schemas/T"}
	inOverlay := ir.Provenance{Source: 1, Pointer: "/components/schemas/S"}

	tests := map[string]struct {
		lowered []ir.Diagnostic
		loaded  []ir.Diagnostic
		want    []ir.Diagnostic
	}{
		"a different code at the same provenance is kept": {
			lowered: []ir.Diagnostic{{Code: diag.DegradedConstruct, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.DegradedConstruct, Provenance: at}},
		},
		"unresolved-ref at a different provenance is kept": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: elsewhere}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: elsewhere}},
		},
		"unresolved-ref at the same pointer in another source is kept": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: inOverlay}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: inOverlay}},
		},
		"unresolved-ref at the same provenance is dropped": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			want:    []ir.Diagnostic{},
		},
		"unresolved-ref where the load phase refused a cycle is dropped": {
			lowered: []ir.Diagnostic{{Code: diag.UnresolvedRef, Provenance: at}},
			loaded:  []ir.Diagnostic{{Code: diag.CyclicRef, Provenance: at}},
			want:    []ir.Diagnostic{},
		},
		"an empty loaded list keeps everything": {
			lowered: []ir.Diagnostic{
				{Code: diag.UnresolvedRef, Provenance: at},
				{Code: diag.DegradedConstruct, Provenance: elsewhere},
			},
			loaded: nil,
			want: []ir.Diagnostic{
				{Code: diag.UnresolvedRef, Provenance: at},
				{Code: diag.DegradedConstruct, Provenance: elsewhere},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := openapi.WithoutRereported(tc.lowered, tc.loaded)
			if d := cmp.Diff(tc.want, got); d != "" {
				t.Errorf("withoutRereported(-want +got):\n%s", d)
			}
		})
	}
}
