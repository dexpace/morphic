package typed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	bd := NewBuilder(bind.Bind(t.Context(), &root, bind.Config{Self: "spec.yaml"}))
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

	bd = NewBuilder(bind.Bind(t.Context(), &root, bind.Config{Self: "spec.yaml"}))
	bd.decode = func(context.Context, *yaml.Node, *schemaRef) error { panic("planted") }
	built = bd.Build(t.Context())
	require.Len(t, built, 1)
	require.Error(t, built[0].Err, "a panic in the decoder is a failure to build, not one that escapes")
	assert.Equal(t, "the parser panicked: planted", built[0].Err.Error())
}

// TestBuild_StopsOnceTheContextIsDone pins Build's and Walk's context: Build
// asks before each landing and Walk before each item of a schema's walk, so a
// done context builds and records nothing, where a live one does both.
func TestBuild_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"+
		"components:\n  schemas:\n    A: {$ref: '#/x-lib/S'}\nx-lib: {S: {properties: {p: {}}}}\n"), &root))
	table := bind.Bind(t.Context(), &root, bind.Config{Self: "spec.yaml"})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Empty(t, NewBuilder(table).Build(cancelled))

	built := NewBuilder(table).Build(t.Context())
	require.NotEmpty(t, built)
	js, at := built[0].Schema, built[0].Landing.At
	require.NotNil(t, js)
	bd := NewBuilder(table)
	bd.Walk(cancelled, js, at)
	assert.Nil(t, bd.Bindings().At(at), "nothing recorded under a done context")
	bd.Walk(t.Context(), js, at)
	assert.Same(t, js, bd.Bindings().At(at))
}

// TestBuild_QuotesAPanicOnOneBoundedLine pins that a panic in the decoder
// reaches the error as diag.Catch quotes it, whatever it panicked with: its
// lines joined and its length bounded, since the load phase reports the error
// as a diagnostic, which is rendered one per line.
func TestBuild_QuotesAPanicOnOneBoundedLine(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("components: {schemas: {A: {$ref: '#/x-lib/S'}}}\nx-lib: {S: {}}\n"), &root))
	huge := strings.Repeat("the parser faulted here\n", 1<<10)
	bd := NewBuilder(bind.Bind(t.Context(), &root, bind.Config{Self: "spec.yaml"}))
	bd.decode = func(context.Context, *yaml.Node, *schemaRef) error { panic(huge) }

	built := bd.Build(t.Context())

	require.Len(t, built, 1)
	require.Error(t, built[0].Err)
	assert.Equal(t, "the parser panicked: "+diag.OneLine(errors.New(huge)), built[0].Err.Error())
	assert.NotContains(t, built[0].Err.Error(), "\n")
	assert.Less(t, len(built[0].Err.Error()), len(huge)/4, "the bound cut the value")
}
