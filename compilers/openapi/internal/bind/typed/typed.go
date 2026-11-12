// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typed

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/speakeasy-api/openapi/walk"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	// of is where each schema at knows was read, by the model it holds, which
	// decoding builds afresh for every path to a node (SiteOf): each where a
	// schema names a dialect, else each writing a list of items (index).
	of map[*oas3.Schema]bind.Position
	// held is the schema at each position: the model's own, or one built.
	held map[bind.Position]*schemaRef
	// unplaced is the reference object each schema and discriminator of an end
	// the table could not place was read under, as spelled (Builder.Unplaced).
	unplaced map[any]string
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

// SiteOf is Site for a reading that holds the model js holds and not js, as
// one of an allOf's conjuncts does: each is decoded afresh for every path to
// its node (docs/parser-library.md#alias-expansion), so it names one position
// where its node may stand at several. It is false before Builder.Build, and,
// where no schema names a dialect, for one writing no list of items (index),
// as each node then reads one dialect and no other reading needs its place.
func (b *Bindings) SiteOf(s *oas3.Schema) (bind.Position, bool) {
	if b == nil || s == nil {
		return bind.Position{}, false
	}
	p, ok := b.of[s]
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
	s, _ := b.DeclaredAt(js)
	return s
}

// DeclaredAt returns what Declared does and the position its $ref lands at,
// which names the document holding it; the zero position where it lands on
// none.
func (b *Bindings) DeclaredAt(js *schemaRef) (*schemaRef, bind.Position) {
	e, ok := b.Entry(js)
	if !ok || e.Hop.Status != bind.Resolved {
		return nil, bind.Position{}
	}
	return b.held[e.Hop.Target], e.Hop.Target
}

// End returns the schema the chain of $refs from js ends on, in whichever
// document, or nil where it does not end on one.
func (b *Bindings) End(js *schemaRef) *schemaRef {
	e, ok := b.Entry(js)
	if !ok || e.Chain.Status != bind.Resolved {
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

// Unplaced returns the reference object in whose content model, a schema or a
// discriminator, was read, spelled as its pointer and, outside the source, its
// document, where the table could not place what the parser resolved that
// object to (bind.Table.Through), so no $ref in it is bound. It is false for
// any other model.
func (b *Bindings) Unplaced(model any) (string, bool) {
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
	decode func(ctx context.Context, n *yaml.Node, js *schemaRef) error
	// bools are the boolean schemas recorded and not yet mended (mend).
	bools []recorded
}

// recorded is a schema, the position it was read at, and how many mendings
// deep it lies (MaxMends).
type recorded struct {
	js    *schemaRef
	at    bind.Position
	mends int
}

// MaxMends bounds how many schemas mend decodes again one below another. Each
// decoding reads the whole schema, so a chain of them costs the square of its
// depth, and one past the bound, a mapping still read as false, is the
// lowering's to report.
const MaxMends = 8

// NewBuilder returns a Builder over table, knowing no schema yet.
func NewBuilder(table *bind.Table) *Builder {
	return &Builder{decode: decodeSchema, view: nodeview.New(), b: &Bindings{table: table, at: map[*schemaRef]bind.Position{},
		discs: map[*oas3.Discriminator]bind.Position{}, held: map[bind.Position]*schemaRef{},
		unplaced: map[any]string{}}}
}

// decodeSchema decodes n into js as the parser decodes a schema of its model.
// What decoding finds wrong with the schema is not kept: the load phase's rule
// table judges the source, and what a reference reaches, itself.
func decodeSchema(ctx context.Context, n *yaml.Node, js *schemaRef) error {
	_, err := marshaller.UnmarshalNode(ctx, "", n, js)
	return err
}

// decodeRecovered runs bd.decode behind diag.Catch and turns a panic in it into
// an error quoting what the parser panicked with: the decoder reads YAML no one
// checked was a schema, and the parser panics on some shapes it accepts
// (docs/parser-library.md#recoverable-panic).
func (bd *Builder) decodeRecovered(ctx context.Context, n *yaml.Node, js *schemaRef) error {
	var err error
	if text, panicked := diag.Catch(func() { err = bd.decode(ctx, n, js) }); panicked {
		return fmt.Errorf("the parser panicked: %s", text)
	}
	return err
}

// Schema records that js was read at p. The first schema recorded at a
// position is the one held there. A boolean one is noted, as the parser
// decodes some written mappings as false (mend).
func (bd *Builder) Schema(js *schemaRef, p bind.Position) {
	if js == nil {
		return
	}
	bd.b.at[js] = p
	if _, held := bd.b.held[p]; !held {
		bd.b.held[p] = js
	}
	if js.IsBool() {
		bd.bools = append(bd.bools, recorded{js: js, at: p})
	}
}

// mend decodes again, in place, each boolean schema recorded whose node is a
// mapping, and records what it holds: the parser decodes such a position as
// false where a keyword beneath it fails to decode under its own name
// (docs/parser-library.md#failure-beneath-itself), though the mapping decoded
// on its own is the schema written. A mended schema is no boolean, so each is
// mended once, and one a mending leaves false below it, as a chain of such
// keywords does, deeper than it, in turn: the work ends within the document's
// nesting. One no decoding reads stays false.
func (bd *Builder) mend(ctx context.Context) {
	for len(bd.bools) > 0 && ctx.Err() == nil {
		r := bd.bools[len(bd.bools)-1]
		bd.bools = bd.bools[:len(bd.bools)-1]
		n := nodeview.Deref(r.js.GetCore().GetRootNode())
		if n == nil || n.Kind != yaml.MappingNode || r.mends >= MaxMends {
			continue
		}
		fresh := &schemaRef{}
		if err := bd.decodeRecovered(ctx, n, fresh); err != nil || fresh.IsBool() {
			continue
		}
		*r.js = *fresh
		below := len(bd.bools)
		bd.Walk(ctx, r.js, r.at)
		for i := below; i < len(bd.bools); i++ {
			bd.bools[i].mends = r.mends + 1
		}
	}
}

// Discriminator records that d was read at p.
func (bd *Builder) Discriminator(d *oas3.Discriminator, p bind.Position) {
	if d != nil {
		bd.b.discs[d] = p
	}
}

// Unplaced records that model, a schema or a discriminator, was read in what
// the reference object site spells resolved to, where the table could not
// place it.
func (bd *Builder) Unplaced(model any, site string) {
	bd.b.unplaced[model] = site
}

// Walk records js, read at p, and every schema and discriminator below it, at
// the position each is read at, until ctx is done: the library's walk reads no
// context (docs/parser-library.md#context-unread), so Walk asks it before each
// item.
func (bd *Builder) Walk(ctx context.Context, js *schemaRef, p bind.Position) {
	for item := range oas3.Walk(ctx, js) {
		if ctx.Err() != nil {
			return
		}
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

// Built is a schema built for a landing: where, and the least position whose
// $ref lands there; Schema is nil where it failed, and Err says why.
type Built struct {
	Landing bind.Landing
	Schema  *schemaRef
	Err     error
}

// Build builds a schema for each of the table's landings, in order, records
// it and everything below it, and returns what each build found. Then each
// schema recorded, before it or by it, that the parser read as false from a
// mapping is mended (mend). It stops between landings once ctx is done, with
// what it built so far, which its caller must not read.
func (bd *Builder) Build(ctx context.Context) []Built {
	landings := bd.b.table.Landings()
	out := make([]Built, 0, len(landings))
	for _, l := range landings {
		if ctx.Err() != nil {
			break
		}
		out = append(out, bd.build(ctx, l))
	}
	bd.mend(ctx)
	bd.index()
	return out
}

// index places each schema recorded by the model it holds (Bindings.SiteOf):
// every one where a schema names a dialect (dialectRooted), as a node an alias
// places twice may then stand under two, and else each writing a list of
// items, whose entries are read where the schema is written (listsItems).
func (bd *Builder) index() {
	rooted := dialectRooted(bd.b.table)
	for js, p := range bd.b.at {
		s := js.GetSchema()
		if s == nil || !rooted && !listsItems(s) {
			continue
		}
		if bd.b.of == nil {
			bd.b.of = map[*oas3.Schema]bind.Position{}
		}
		bd.b.of[s] = p
	}
}

// listsItems reports whether s writes `items` as a list, a tuple as the
// JSON Schema drafts before 2020-12 spell one, whose entries the parser's
// model holds none of: the load phase builds them at their positions.
func listsItems(s *oas3.Schema) bool {
	n := nodeview.Child(s.GetRootNode(), "items")
	return n != nil && n.Kind == yaml.SequenceNode
}

// dialectRooted reports whether a schema of a document t read names a
// dialect by `$schema`, the one way a node an alias places twice stands under
// two: with none, each node reads its own document's, which its node tells.
func dialectRooted(t *bind.Table) bool {
	for id := range bind.DocID(t.Documents()) {
		if len(t.Document(id).Dialects) > 0 {
			return true
		}
	}
	return false
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
	if err := bd.decodeRecovered(ctx, path[len(path)-1], js); err != nil {
		built.Err = err
		return built
	}
	built.Schema = js
	bd.Walk(ctx, js, l.At)
	return built
}

// Bindings returns what bd has built. bd is not used after.
func (bd *Builder) Bindings() *Bindings { return bd.b }
