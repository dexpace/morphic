package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// allKinds is the closed kind set from ir-design §4. Adding a TypeKind without
// updating every consumer must break this file (the assertNever lesson).
var allKinds = []ir.TypeKind{
	ir.KindPrimitive, ir.KindScalar, ir.KindModel, ir.KindUnion, ir.KindEnum,
	ir.KindList, ir.KindMap, ir.KindTuple, ir.KindLiteral, ir.KindExternal, ir.KindAny,
}

func TestTypeDef_KindDispatchIsComplete(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type registered for kind %q", k)
			assert.Equal(t, k, td.Kind())
			require.NotNil(t, td.Common())
		})
	}
}

func TestNewTypeDef_UnknownKind(t *testing.T) {
	t.Parallel()
	_, ok := ir.NewTypeDef(ir.TypeKind("bogus"))
	assert.False(t, ok)
}

// allConcreteTypeDefs names the sum's members as concrete types rather than as
// kinds, so a type that stops satisfying ir.TypeDef fails to compile here.
// TestTypeDef_HandWrittenConcreteListIsComplete holds the list to the sealed
// marker set derived from the ir sources: nothing that compares hand-written
// lists against each other can see a variant absent from all of them.
var allConcreteTypeDefs = []ir.TypeDef{
	&ir.Primitive{}, &ir.Scalar{}, &ir.Model{}, &ir.Union{}, &ir.Enum{},
	&ir.List{}, &ir.MapT{}, &ir.Tuple{}, &ir.Literal{}, &ir.External{}, &ir.Any{},
}

func TestTypeDef_ConcreteTypesImplementInterface(t *testing.T) {
	t.Parallel()
	for _, td := range allConcreteTypeDefs {
		assert.Contains(t, allKinds, td.Kind())
	}
}
