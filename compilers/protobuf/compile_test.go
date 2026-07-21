package protobuf_test // external test package — exercises only the public API

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/protobuf"
	"github.com/dexpace/morphic/ir"
)

const wrappersProto = `syntax = "proto3";
package w;
import "google/protobuf/wrappers.proto";
message Box {
  google.protobuf.StringValue note = 1;
}
`

func compile(t *testing.T, path, src string, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	t.Helper()
	return protobuf.New().Compile(context.Background(),
		[]compilers.Source{{Path: path, Data: []byte(src)}}, opts)
}

func TestCompile_EndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags, err := compile(t, "w.proto", wrappersProto, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
	assert.Equal(t, ir.IRVersion, doc.IRVersion)
	assert.Equal(t, "w", doc.Name)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Sources, 1)
	assert.Equal(t, "protobuf@3", doc.Sources[0].Format)
	assert.Len(t, doc.Sources[0].Hash, 64)
}

func TestCompile_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/golden/protobuf/library.proto")
	require.NoError(t, err)
	doc, _, err := compile(t, "library.proto", string(data), compilers.Options{})
	require.NoError(t, err)
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

func TestCompile_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(protobuf.New()))
	for _, ver := range []string{"2", "3", "2023"} {
		got, ok := reg.Lookup(compilers.SourceFormat{Name: "protobuf", Version: ver})
		require.True(t, ok, "format protobuf@%s registered", ver)
		assert.NotNil(t, got)
	}
}

func TestCompile_RejectsMultipleSources(t *testing.T) {
	t.Parallel()
	_, _, err := protobuf.New().Compile(context.Background(),
		[]compilers.Source{
			{Path: "a.proto", Data: []byte(wrappersProto)},
			{Path: "b.proto", Data: []byte(wrappersProto)},
		}, compilers.Options{})
	require.Error(t, err)
}

func TestCompile_RejectsWrongOptions(t *testing.T) {
	t.Parallel()
	_, _, err := protobuf.New().Compile(context.Background(),
		[]compilers.Source{{Path: "w.proto", Data: []byte(wrappersProto)}},
		compilers.Options{FormatOptions: "nonsense"})
	require.Error(t, err)
}

func TestCompile_UnresolvedImport(t *testing.T) {
	t.Parallel()
	const src = `syntax = "proto3";
package u;
import "other/thing.proto";
message M {
  other.Thing t = 1;
}
`
	doc, diags, err := compile(t, "u.proto", src, compilers.Options{})
	require.NoError(t, err, "an unresolved import is a spec problem, not a Go error")
	assert.Nil(t, doc, "the compiler refuses to lower a document it cannot link")
	var found bool
	for _, d := range diags {
		if d.Code == "protobuf/unresolved-import" {
			found = true
			assert.Equal(t, ir.SeverityError, d.Severity)
		}
	}
	assert.True(t, found, "unresolved import reported as a diagnostic")
}

func TestCompile_WrapperExternalPolicy(t *testing.T) {
	t.Parallel()
	doc, _, err := compile(t, "w.proto", wrappersProto,
		compilers.Options{FormatOptions: protobuf.Options{Wrappers: protobuf.WrapperExternal}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	box, ok := doc.Types[ir.TypeID("t/protobuf/w.Box")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, box.Properties, 1)
	target := box.Properties[0].Type.Target
	ext, ok := doc.Types[target].(*ir.External)
	require.True(t, ok, "WrapperExternal policy lowers wrappers to External")
	assert.Equal(t, "google.protobuf.StringValue", ext.Identity)
}

func TestCompile_SyntaxError(t *testing.T) {
	t.Parallel()
	const src = `syntax = "proto3";
package s;
message M {
  string s = ;
}
`
	doc, diags, err := compile(t, "s.proto", src, compilers.Options{})
	require.NoError(t, err, "a syntax error is a spec problem, not a Go error")
	assert.Nil(t, doc, "a malformed source cannot be lowered")
	var found bool
	for _, d := range diags {
		if d.Code == "protobuf/compile-error" && d.Severity == ir.SeverityError {
			found = true
			assert.Regexp(t, `^\d+:\d+$`, d.Provenance.Pointer, "parse errors carry line:col provenance")
		}
	}
	assert.True(t, found, "syntax error reported with position")
}

func TestCompile_ImportWarning(t *testing.T) {
	t.Parallel()
	const src = `syntax = "proto3";
package iw;
import "google/protobuf/empty.proto";
message M {
  string s = 1;
}
`
	doc, diags, err := compile(t, "iw.proto", src, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "an unused import is a warning, not a hard failure")
	var found bool
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning && d.Code == "protobuf/warning" {
			found = true
		}
	}
	assert.True(t, found, "an unused import surfaces as a warning diagnostic")
}

func TestCompile_SyntaxDigit(t *testing.T) {
	t.Parallel()
	const proto2Src = `syntax = "proto2";
package p2;
message M {
  required string id = 1;
}
`
	doc, _, err := compile(t, "p2.proto", proto2Src, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "protobuf@2", doc.Sources[0].Format)
}
