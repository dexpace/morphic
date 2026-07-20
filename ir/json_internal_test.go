package ir

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTypeDef_MarkerMethods exercises the unexported sealed-interface marker on
// every concrete TypeDef kind. The markers carry no behavior; calling them keeps
// the sealed-sum contract honest and observable to coverage.
func TestTypeDef_MarkerMethods(t *testing.T) {
	t.Parallel()
	defs := []TypeDef{
		&Primitive{},
		&Scalar{},
		&Model{},
		&Union{},
		&Enum{},
		&List{},
		&MapT{},
		&Tuple{},
		&Literal{},
		&External{},
		&Any{},
	}
	for _, td := range defs {
		t.Run(string(td.Kind()), func(t *testing.T) {
			t.Parallel()
			// typeDef is the unexported sealing marker on the TypeDef interface;
			// invoking it keeps the sealed-sum contract observable. The bodies are
			// empty (zero statements), so this documents rather than covers them.
			td.typeDef()
		})
	}
}

// TestMarshalWithKind_MarshalError drives the json.Marshal failure branch by
// handing marshalWithKind a value encoding/json cannot encode.
func TestMarshalWithKind_MarshalError(t *testing.T) {
	t.Parallel()
	_, err := marshalWithKind(KindAny, make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal any")
}

// TestMarshalWithKind_NonObject drives the "must encode as an object" guard by
// handing marshalWithKind a value that encodes as a JSON scalar/array.
func TestMarshalWithKind_NonObject(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"number": 42,
		"array":  []int{1},
		"string": "x",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := marshalWithKind(KindPrimitive, v)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must encode as an object")
		})
	}
}

// TestMarshalWithKind_EmptyObject drives the empty-object branch: a value that
// encodes as "{}" must still receive its adjacent kind tag and no stray comma.
func TestMarshalWithKind_EmptyObject(t *testing.T) {
	t.Parallel()
	out, err := marshalWithKind(KindAny, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, `{"kind":"any"}`, string(out))
}

// TestMarshalWithKind_NonEmptyObject confirms the populated-object branch splices
// fields after the kind tag.
func TestMarshalWithKind_NonEmptyObject(t *testing.T) {
	t.Parallel()
	out, err := marshalWithKind(KindPrimitive, struct {
		Prim string `json:"prim"`
	}{Prim: "string"})
	require.NoError(t, err)
	got := string(out)
	assert.True(t, strings.HasPrefix(got, `{"kind":"primitive",`), "kind tag leads: %s", got)
	assert.Contains(t, got, `"prim":"string"`)
}
