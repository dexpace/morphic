// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestIDGrammar_KindPrefixes pins the spelling of every ID kind, because the
// prefix is what a consumer reading IDs from more than one compiler switches on.
// The expected strings are written out rather than derived: a test that built
// them the way the code does would agree with any change to it.
func TestIDGrammar_KindPrefixes(t *testing.T) {
	t.Parallel()
	const space compile.Space = "openapi"
	const pointer = "/components/schemas/User"

	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/User"), compile.TypeID(space, pointer))
	assert.Equal(t, ir.OpID("op/openapi/paths/~1pets/get"), compile.OpID(space, "/paths/~1pets/get"))
	assert.Equal(t, ir.PropID("p/openapi/components/schemas/User/properties/id"),
		compile.PropID(space, "/components/schemas/User/properties/id"))
	assert.Equal(t, ir.AuthID("auth/openapi/components/securitySchemes/apiKey"),
		compile.AuthID(space, "/components/securitySchemes/apiKey"))
	assert.Equal(t, ir.ServiceID("s/openapi/0"), compile.ServiceID(space, "0"))
	assert.Equal(t, ir.GroupID("g/tags/pets"), compile.GroupID("tags", "/pets"))
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1pets/get/parameters/limit/query"),
		compile.ParamID(space, "/paths/~1pets/get/parameters/limit/query"))
}

// TestEnumMemberID_UsesTheEnumMemberKind pins the kind an enum member's ID opens
// with, written out so it cannot agree with a change to the constant.
func TestEnumMemberID_UsesTheEnumMemberKind(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/Status/s:active"),
		compile.EnumMemberID("openapi", "/components/schemas/Status/s:active"))
	assert.Equal(t, "e", ir.IDKindEnumMember)
}

// TestServerID_UsesTheServerKind pins the kind a server's ID opens with,
// written out so it cannot agree with a change to the constant, and that a space
// alone names one server, as a minted default is.
func TestServerID_UsesTheServerKind(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.ServerID("server/openapi/servers/0"), compile.ServerID("openapi", "/servers/0"))
	assert.Equal(t, ir.ServerID("server/default"), compile.ServerID("default", ""))
	assert.Equal(t, "server", ir.IDKindServer)
}

// TestIDGrammar_PathSeparatorIsSuppliedOnce pins that the framework owns the
// boundary between the space and the path. A format whose paths carry a leading
// separator (an RFC 6901 pointer) and one whose paths do not (a protobuf
// fully-qualified name) must reach the same shape, and neither may run the space
// into the path.
func TestIDGrammar_PathSeparatorIsSuppliedOnce(t *testing.T) {
	t.Parallel()
	const space compile.Space = "protobuf"

	assert.Equal(t, compile.TypeID(space, "/example.v1.User"), compile.TypeID(space, "example.v1.User"))
	assert.Equal(t, ir.TypeID("t/protobuf/example.v1.User"), compile.TypeID(space, "example.v1.User"))
	assert.Equal(t, ir.TypeID("t/protobuf"), compile.TypeID(space, ""),
		"a space naming one node gets no trailing separator")
}

// TestSpace_QualifiedNamesTheDocumentInOneSegment pins how a space names
// another document: after an '@', with '~' and '/' escaped as a pointer token
// escapes them, '~' first. The ID it opens keeps the path's separator, and
// documents differing only in where a '/' or '~' falls qualify the space
// apart. Expected strings are written out, not derived.
func TestSpace_QualifiedNamesTheDocumentInOneSegment(t *testing.T) {
	t.Parallel()
	const space compile.Space = "openapi"
	for _, tc := range []struct{ document, want string }{
		{"common.yaml", "openapi@common.yaml"},
		{"schemas/pet.yaml", "openapi@schemas~1pet.yaml"},
		{"../shared/a~b.yaml", "openapi@..~1shared~1a~0b.yaml"},
		{"a~1b.yaml", "openapi@a~01b.yaml"},
		{"https://example.com/x.yaml", "openapi@https:~1~1example.com~1x.yaml"},
	} {
		assert.Equal(t, compile.Space(tc.want), space.Qualified(tc.document), tc.document)
	}
	id := string(compile.TypeID(space.Qualified("schemas/pet.yaml"), "/Pet/properties/x"))
	got, ok := ir.IDSpace(ir.IDKindType, id)
	require.True(t, ok)
	assert.Equal(t, "openapi@schemas~1pet.yaml", got)
	path, ok := ir.IDPath(ir.IDKindType, id)
	require.True(t, ok)
	assert.Equal(t, "Pet/properties/x", path, "the path keeps its own separator")
	assert.NotEqual(t, space.Qualified("a/b"), space.Qualified("a~1b"))
}

// TestTypes_MintingIntoASourceSpaceIsRefused plants invariant 3's corollary
// broken — a minted node taking a namespace that addresses source coordinates —
// and asserts it is refused whichever order the two arrive in.
//
// Both orders are asserted because that is the failure's whole character: the
// document is well-formed either way and only the winner of the collision
// changes, so a single-order test passes on a compiler that is already wrong.
func TestTypes_MintingIntoASourceSpaceIsRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(types *compile.Types)
	}{
		{"source coordinate first", func(types *compile.Types) {
			types.Intern("/components/schemas/User", "t/openapi/components/schemas/User", model)
			types.Register("t/openapi/components/schemas/User/oneOf/0", model())
		}},
		{"minted node first", func(types *compile.Types) {
			types.Register("t/openapi/components/schemas/User/oneOf/0", model())
			types.Intern("/components/schemas/User", "t/openapi/components/schemas/User", model)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			tc.run(types)
			require.Len(t, types.Violations(), 1, "the collision is reported exactly once")
			assert.Contains(t, types.Violations()[0], `namespace "openapi"`)
			assert.Contains(t, types.Violations()[0], "needs a namespace of its own")
		})
	}
}

// TestTypes_SeparateSpacesAreNotRefused is the negative control: the arrangement
// the OpenAPI compiler actually uses — coordinates in openapi and anon, minted
// variants in composed — must stay silent, or the check above would be reporting
// on every compile.
func TestTypes_SeparateSpacesAreNotRefused(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Intern("/components/schemas/User", "t/openapi/components/schemas/User", model)
	types.Intern("/components/schemas/User/properties/tags", "t/anon/components/schemas/User/properties/tags", model)
	types.Register("t/composed/components/schemas/User/oneOf/0", model())
	types.Register("t/composed/components/schemas/User/oneOf/1", model())

	assert.Empty(t, types.Violations())
	assert.Equal(t, 4, types.Len())
}

// TestTypes_IDWithNoSpaceSegmentClaimsNothing covers the one shape the namespace
// check cannot judge: an ID that was not built here, or was built with an empty
// Space. It is left alone rather than refused, because an ID with no namespace
// cannot be a namespace used two ways.
func TestTypes_IDWithNoSpaceSegmentClaimsNothing(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Register("bare", model())
	types.Intern("/p", "bare", model)

	assert.Empty(t, types.Violations())
}

// model returns a distinct empty Model, the simplest node the registry accepts.
func model() ir.TypeDef { return &ir.Model{} }
