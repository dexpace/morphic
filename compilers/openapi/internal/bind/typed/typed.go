// Package typed bridges a bind table to what the lowering reads: the typed
// schema the parser's model holds at a position, or one built from raw YAML
// there, built once per position.
package typed

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// Bridge answers a position with its typed schema.
type Bridge struct {
	doc   any
	root  *yaml.Node
	view  *nodeview.View
	built map[jsontext.Pointer]*schemaRef
	// decode builds a schema from raw YAML: the parser's own decoder, which a
	// test replaces to reach its failure.
	decode func(ctx context.Context, n *yaml.Node, js *schemaRef) error
}

// New returns a Bridge over doc, the parser's model, and root, the tree it was
// built from.
func New(doc any, root *yaml.Node) *Bridge {
	return &Bridge{doc: doc, root: nodeview.DocumentRoot(root), view: nodeview.New(),
		built: map[jsontext.Pointer]*schemaRef{}, decode: decodeSchema}
}

// decodeSchema decodes n into js as the parser decodes a schema it resolves to
// raw YAML, its findings left to validation.
func decodeSchema(ctx context.Context, n *yaml.Node, js *schemaRef) error {
	_, err := marshaller.UnmarshalNode(ctx, "", n, js)
	return err
}

// Schema returns the typed schema at pointer, a position of the given role:
// the model's own for a Schema, one built from the YAML for Raw. It reports an
// error for any other role, or a position that holds none.
func (b *Bridge) Schema(ctx context.Context, pointer jsontext.Pointer, role bind.Role) (*schemaRef, error) {
	switch role {
	case bind.Schema:
		return b.model(pointer)
	case bind.Raw:
		return b.raw(ctx, pointer)
	default:
		return nil, fmt.Errorf("position %s holds no schema (%s)", pointer, role)
	}
}

// model is the schema the parser's model holds at pointer.
func (b *Bridge) model(pointer jsontext.Pointer) (*schemaRef, error) {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return nil, fmt.Errorf("pointer %s is not one the model reads", pointer)
	}
	at, rest, err := navigation.Walk(b.doc, tokens)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", pointer, err)
	}
	js, isSchema := at.(*schemaRef)
	if len(rest) != 0 || !isSchema || js == nil {
		return nil, fmt.Errorf("the model holds no schema at %s", pointer)
	}
	return js, nil
}

// raw is the schema built from the YAML at pointer, once.
func (b *Bridge) raw(ctx context.Context, pointer jsontext.Pointer) (*schemaRef, error) {
	if js, ok := b.built[pointer]; ok {
		return js, nil
	}
	path, complete := b.view.DocumentPath(b.root, pointer)
	if !complete {
		return nil, fmt.Errorf("the document holds nothing at %s", pointer)
	}
	js := &schemaRef{}
	if err := b.decode(ctx, path[len(path)-1], js); err != nil {
		return nil, fmt.Errorf("building the schema at %s: %w", pointer, err)
	}
	b.built[pointer] = js
	return js, nil
}
