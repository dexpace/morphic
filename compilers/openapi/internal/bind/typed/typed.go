// Package typed answers what the lowering asks of a bind table about the
// typed schemas it holds: where a schema's $ref lands, the schema written
// there and the one its chain ends on. It knows each schema the lowering can
// hold by the position it was read at, and builds a schema once for each
// position a $ref reads as one though the parser's model holds none there.
package typed

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/speakeasy-api/openapi/walk"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// Bindings is a bind table and the typed schemas its positions hold. It is
// built once (Builder) and never written after, and a nil one knows nothing.
type Bindings struct {
	table *bind.Table
	// at is where each schema, and each discriminator, the lowering can hold
	// was read.
	at    map[*schemaRef]bind.Position
	discs map[*oas3.Discriminator]bind.Position
	// held is the schema at each position: the model's own, or one built.
	held map[bind.Position]*schemaRef
	// unplaced is the source's reference object each schema and discriminator
	// of an end the table could not place was read under (Builder.Unplaced).
	unplaced map[any]jsontext.Pointer
}

// Table returns the table b answers from.
func (b *Bindings) Table() *bind.Table {
	if b == nil {
		return nil
	}
	return b.table
}

// Site returns the position js was read at, and false for a schema b does not
// know.
func (b *Bindings) Site(js *schemaRef) (bind.Position, bool) {
	if b == nil || js == nil {
		return bind.Position{}, false
	}
	p, ok := b.at[js]
	return p, ok
}

// Entry returns the table's entry for the $ref js writes, and false for a
// schema b does not know or one the table read no $ref at.
func (b *Bindings) Entry(js *schemaRef) (bind.Entry, bool) {
	p, ok := b.Site(js)
	if !ok {
		return bind.Entry{}, false
	}
	return b.table.Entry(p)
}

// Declared returns the schema written where js's $ref lands, or nil where it
// does not land on one.
func (b *Bindings) Declared(js *schemaRef) *schemaRef {
	e, ok := b.Entry(js)
	if !ok || e.Hop.Status != bind.Resolved {
		return nil
	}
	return b.held[e.Hop.Target]
}

// End returns the schema the chain of $refs from js ends on, or nil where it
// does not end on one or js's $ref is written in another document. The
// parser's resolver resolved no $ref there (GitHub #751), so what one names
// lends the use site holding it nothing: no annotation, form part, file body
// or discriminator.
func (b *Bindings) End(js *schemaRef) *schemaRef {
	e, ok := b.Entry(js)
	if !ok || e.Chain.Status != bind.Resolved || e.Site.Doc != bind.Source {
		return nil
	}
	return b.held[e.Chain.End]
}

// At returns the schema held at p, or nil.
func (b *Bindings) At(p bind.Position) *schemaRef {
	if b == nil {
		return nil
	}
	return b.held[p]
}

// Mapping returns where value, a mapping value of the discriminator d, lands,
// and false for a discriminator b does not know or a value the table did not
// read as a reference.
func (b *Bindings) Mapping(d *oas3.Discriminator, value string) (bind.Mapping, bool) {
	if b == nil || d == nil {
		return bind.Mapping{}, false
	}
	at, ok := b.discs[d]
	if !ok {
		return bind.Mapping{}, false
	}
	return b.table.Mapping(at, value)
}

// Unplaced returns the pointer of the source's reference object in whose
// content model, a schema or a discriminator, was read, where the table could
// not place what the parser resolved that object to (bind.Table.Through), so
// no $ref in it is bound. It is false for any other model.
func (b *Bindings) Unplaced(model any) (jsontext.Pointer, bool) {
	if b == nil {
		return "", false
	}
	site, ok := b.unplaced[model]
	return site, ok
}

// Builder builds Bindings over a table: told where each schema of the
// parser's model was read, it builds a schema for each landing.
type Builder struct {
	b *Bindings
	// view reads every landing's path, so a mapping many landings sit in is
	// expanded once rather than once a landing.
	view *nodeview.View
	// decode builds a schema from YAML: the parser's own decoder, which a test
	// replaces to reach its failure.
	decode func(ctx context.Context, n *yaml.Node, js *schemaRef) ([]error, error)
}

// NewBuilder returns a Builder over table, knowing no schema yet.
func NewBuilder(table *bind.Table) *Builder {
	return &Builder{decode: decodeSchema, view: nodeview.New(), b: &Bindings{table: table, at: map[*schemaRef]bind.Position{},
		discs: map[*oas3.Discriminator]bind.Position{}, held: map[bind.Position]*schemaRef{},
		unplaced: map[any]jsontext.Pointer{}}}
}

// decodeSchema decodes n into js as the parser decodes a schema of its model,
// returning the findings decoding it raises.
func decodeSchema(ctx context.Context, n *yaml.Node, js *schemaRef) ([]error, error) {
	return marshaller.UnmarshalNode(ctx, "", n, js)
}

// decodeRecovered is bd.decode with a panic on the caller's goroutine turned
// into an error: the decoder reads YAML no one checked was a schema, and the
// parser panics on some shapes it accepts
// (docs/parser-library.md#recoverable-panic).
func (bd *Builder) decodeRecovered(ctx context.Context, n *yaml.Node, js *schemaRef) (findings []error, err error) {
	defer func() {
		if r := recover(); r != nil {
			findings, err = nil, fmt.Errorf("the parser panicked: %v", r)
		}
	}()
	return bd.decode(ctx, n, js)
}

// Schema records that js was read at p. The first schema recorded at a
// position is the one held there.
func (bd *Builder) Schema(js *schemaRef, p bind.Position) {
	if js == nil {
		return
	}
	bd.b.at[js] = p
	if _, held := bd.b.held[p]; !held {
		bd.b.held[p] = js
	}
}

// Discriminator records that d was read at p.
func (bd *Builder) Discriminator(d *oas3.Discriminator, p bind.Position) {
	if d != nil {
		bd.b.discs[d] = p
	}
}

// Unplaced records that model, a schema or a discriminator, was read in what
// the source's reference object at site resolved to, where the table could
// not place it.
func (bd *Builder) Unplaced(model any, site jsontext.Pointer) {
	bd.b.unplaced[model] = site
}

// Walk records js, read at p, and every schema and discriminator below it, at
// the position each is read at.
func (bd *Builder) Walk(ctx context.Context, js *schemaRef, p bind.Position) {
	for item := range oas3.Walk(ctx, js) {
		at := bind.Position{Doc: p.Doc, Pointer: p.Pointer + PointerOf(item.Location)}
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(oas3.SchemaMatcher{
			Schema:        func(s *schemaRef) error { bd.Schema(s, at); return nil },
			Discriminator: func(d *oas3.Discriminator) error { bd.Discriminator(d, at); return nil },
		})
	}
}

// PointerOf is the JSON pointer locations spell, relative to where the walk
// that yielded them started. The library's own rendering writes a lone '/' for
// none and '//' before a walk's first key, which name other positions. It is
// spelled once, in steps linear in its length: extending a pointer a token at
// a time copies it at each, which costs the square of a deep schema's depth.
func PointerOf[T any](locations walk.Locations[T]) jsontext.Pointer {
	var out []byte
	for _, l := range locations {
		if l.ParentField != "" {
			out = appendToken(out, l.ParentField)
		}
		switch {
		case l.ParentKey != nil:
			out = appendToken(out, *l.ParentKey)
		case l.ParentIndex != nil:
			out = appendToken(out, strconv.Itoa(*l.ParentIndex))
		}
	}
	return jsontext.Pointer(out)
}

// appendToken appends token to the pointer spelled in b, escaped as
// jsontext.Pointer.AppendToken escapes one.
func appendToken(b []byte, token string) []byte {
	return append(b, jsontext.Pointer("").AppendToken(token)...)
}

// errNothingThere is why no schema is built for a landing whose document
// holds nothing at its position, which no table reports.
var errNothingThere = errors.New("the document holds nothing there")

// Built is a schema built for a landing: where, the least position whose $ref
// lands there, and what decoding it found; Schema is nil where it failed, and
// Err says why.
type Built struct {
	Landing  bind.Landing
	Schema   *schemaRef
	Findings []error
	Err      error
}

// Build builds a schema for each of the table's landings, in order, records
// it and everything below it, and returns what each build found.
func (bd *Builder) Build(ctx context.Context) []Built {
	landings := bd.b.table.Landings()
	out := make([]Built, 0, len(landings))
	for _, l := range landings {
		out = append(out, bd.build(ctx, l))
	}
	return out
}

// build builds the schema at l, unless one is held there already: one inside
// a landing built before it, or what a reference object of the source
// resolved to, which the parser built.
func (bd *Builder) build(ctx context.Context, l bind.Landing) Built {
	built := Built{Landing: l, Schema: bd.b.held[l.At]}
	if built.Schema != nil {
		return built
	}
	doc := bd.b.table.Document(l.At.Doc)
	path, complete := bd.view.DocumentPath(doc.Root, l.At.Pointer)
	if !complete {
		built.Err = errNothingThere
		return built
	}
	js := &schemaRef{}
	findings, err := bd.decodeRecovered(ctx, path[len(path)-1], js)
	if err != nil {
		built.Err = err
		return built
	}
	built.Schema, built.Findings = js, findings
	bd.Walk(ctx, js, l.At)
	return built
}

// Bindings returns what bd has built. bd is not used after.
func (bd *Builder) Bindings() *Bindings { return bd.b }
