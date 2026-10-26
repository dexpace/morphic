package typed

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestBuild_ReportsAFailureAndHoldsNothing pins the ways building a landing
// fails: the decoder refusing the node or panicking on it, and a landing whose
// document holds nothing at its position, which no table reports. Each is
// reported with the position, and nothing is held there.
func TestBuild_ReportsAFailureAndHoldsNothing(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"+
		"components:\n  schemas:\n    A: {$ref: '#/x-lib/S'}\nx-lib: {S: {}}\n"), &root))
	bd := NewBuilder(bind.Bind(&root, bind.Config{Self: "spec.yaml"}))
	bd.decode = func(context.Context, *yaml.Node, *schemaRef) error { return errors.New("planted") }

	built := bd.Build(t.Context())
	require.Len(t, built, 1)
	require.Error(t, built[0].Err)
	assert.Equal(t, "planted", built[0].Err.Error())
	assert.Nil(t, built[0].Schema)
	assert.Nil(t, bd.Bindings().At(bind.Position{Pointer: "/x-lib/S"}))

	gone := bd.build(t.Context(), bind.Landing{At: bind.Position{Pointer: "/nowhere"}})
	require.Error(t, gone.Err)
	assert.ErrorIs(t, gone.Err, errNothingThere)

	bd = NewBuilder(bind.Bind(&root, bind.Config{Self: "spec.yaml"}))
	bd.decode = func(context.Context, *yaml.Node, *schemaRef) error { panic("planted") }
	built = bd.Build(t.Context())
	require.Len(t, built, 1)
	require.Error(t, built[0].Err, "a panic in the decoder is a failure to build, not one that escapes")
	assert.Equal(t, "the parser panicked: planted", built[0].Err.Error())
}
