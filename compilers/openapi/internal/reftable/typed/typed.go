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

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// Bindings is a bind table and the typed models its positions hold: schemas,
// and the reference-or-inline wrappers of the parser's other kinds. It is
// built once (Builder) and never written after, and a nil one knows nothing.
type Bindings struct {
	table *reftable.ReferenceTargetTable
	// at is where each schema the lowering can hold was read.
	at map[*schemaRef]reftable.DocumentPointer
	// held is the schema at each position: the model's own, or one built.
	held map[reftable.DocumentPointer]*schemaRef
	// objectSites is where each wrapper was read, and as which kind, and
	// objects the wrapper of each kind held at each position: the model's own,
	// or one built for an object landing.
	objectSites map[any]objectSite
	objects     map[objectSite]any
}

// objectSite is a position and the kind of object read there.
type objectSite struct {
	at   reftable.DocumentPointer
	kind reftable.RefKind
}

// Table returns the table b answers from.
func (b *Bindings) Table() *reftable.ReferenceTargetTable {
	if b == nil {
		return nil
	}
	return b.table
}

// Site returns the position js was read at, and false for a schema b does not
// know.
func (b *Bindings) Site(js *schemaRef) (reftable.DocumentPointer, bool) {
	if b == nil || js == nil {
		return reftable.DocumentPointer{}, false
	}
	documentPointer, ok := b.at[js]
	return documentPointer, ok
}

// Entry returns the table's entry for the $ref js writes, and false for a
// schema b does not know or one the table read no $ref at.
func (b *Bindings) Entry(js *schemaRef) (reftable.ReferenceSite, bool) {
	documentPointer, ok := b.Site(js)
	if !ok {
		return reftable.ReferenceSite{}, false
	}
	return b.table.Entry(documentPointer)
}

// RefKeyword returns the keyword js is a reference site under, which the
// lowering reads through the table (Entry) rather than as a body: "$ref" for a
// schema writing one, an empty one included, and "$dynamicRef" for one whose
// $dynamicRef the table reads as the $ref it spells, as JSON Schema 2020-12
// §8.2.3.2 resolves one naming no $dynamicAnchor
// (docs/compilers/openapi.md#dynamic-ref). It is empty for any other schema.
// A reader asks it rather than the model's $ref, so the two spellings lower
// alike.
func (b *Bindings) RefKeyword(js *schemaRef) string {
	if js == nil || js.IsBool() {
		return ""
	}
	if js.GetSchema().Ref != nil {
		return "$ref"
	}
	if referenceSite, ok := b.Entry(js); ok && referenceSite.Keyword == "$dynamicRef" {
		return referenceSite.Keyword
	}
	return ""
}

// RefSite reports whether js is a reference site: one RefKeyword names a
// keyword for.
func (b *Bindings) RefSite(js *schemaRef) bool { return b.RefKeyword(js) != "" }

// Declared returns the schema written where js's $ref lands, or nil where it
// does not land on one.
func (b *Bindings) Declared(js *schemaRef) *schemaRef {
	s, _ := b.DeclaredAt(js)
	return s
}

// DeclaredAt returns what Declared does and the position its $ref lands at,
// which names the document holding it; the zero position where it lands on
// none.
func (b *Bindings) DeclaredAt(js *schemaRef) (*schemaRef, reftable.DocumentPointer) {
	referenceSite, ok := b.Entry(js)
	if !ok || referenceSite.Hop.Status != reftable.Resolved {
		return nil, reftable.DocumentPointer{}
	}
	return b.held[referenceSite.Hop.Target], referenceSite.Hop.Target
}

// End returns the schema the chain of $refs from js ends on, in whichever
// document, or nil where it does not end on one.
func (b *Bindings) End(js *schemaRef) *schemaRef {
	referenceSite, ok := b.Entry(js)
	if !ok || referenceSite.Chain.Status != reftable.Resolved {
		return nil
	}
	return b.held[referenceSite.Chain.End]
}

// At returns the schema held at documentPointer, or nil.
func (b *Bindings) At(documentPointer reftable.DocumentPointer) *schemaRef {
	if b == nil {
		return nil
	}
	return b.held[documentPointer]
}

// Mapping returns where value, a mapping value of the discriminator read at
// at, lands, and false for a value the table did not read as a reference
// there.
func (b *Bindings) Mapping(at reftable.DocumentPointer, value string) (reftable.Mapping, bool) {
	if b == nil {
		return reftable.Mapping{}, false
	}
	return b.table.Mapping(at, value)
}

// ObjectSite returns where model, a reference-or-inline wrapper, was read, and
// as which kind, and false for one b does not know.
func (b *Bindings) ObjectSite(model any) (reftable.DocumentPointer, reftable.RefKind, bool) {
	if b == nil {
		return reftable.DocumentPointer{}, 0, false
	}
	objectSite, ok := b.objectSites[model]
	return objectSite.at, objectSite.kind, ok
}

// Reference returns the table's entry for the Reference Object model's site
// writes, read as the kind model was read as, and false where the table reads
// none there: an inline object, or a wrapper b does not know.
func (b *Bindings) Reference(model any) (reftable.Reference, bool) {
	at, kind, ok := b.ObjectSite(model)
	if !ok {
		return reftable.Reference{}, false
	}
	return b.table.Reference(at, kind)
}

// ObjectAt returns the wrapper of kind held at documentPointer, the first recorded there, or
// nil.
func (b *Bindings) ObjectAt(documentPointer reftable.DocumentPointer, kind reftable.RefKind) any {
	if b == nil {
		return nil
	}
	return b.objects[objectSite{at: documentPointer, kind: kind}]
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
	decode func(ctx context.Context, node *yaml.Node, js *schemaRef) error
	// bools are the boolean schemas recorded and not yet mended (mend).
	bools []recorded
}

// recorded is a schema, the position it was read at, and how many mendings
// deep it lies (MaxMends).
type recorded struct {
	js    *schemaRef
	at    reftable.DocumentPointer
	mends int
}

// MaxMends bounds how many schemas mend decodes again one below another. Each
// decoding reads the whole schema, so a chain of them costs the square of its
// depth, and one past the bound, a mapping still read as false, is the
// lowering's to report.
const MaxMends = 8

// NewBuilder returns a Builder over table, knowing no schema yet.
func NewBuilder(table *reftable.ReferenceTargetTable) *Builder {
	return &Builder{decode: decodeSchema, view: nodeview.New(), b: &Bindings{table: table, at: map[*schemaRef]reftable.DocumentPointer{},
		held:        map[reftable.DocumentPointer]*schemaRef{},
		objectSites: map[any]objectSite{}, objects: map[objectSite]any{}}}
}

// decodeSchema decodes node into js as the parser decodes a schema of its model.
// What decoding finds wrong with the schema is not kept: the load phase's rule
// table judges the source, and what a reference reaches, itself.
func decodeSchema(ctx context.Context, node *yaml.Node, js *schemaRef) error {
	_, err := marshaller.UnmarshalNode(ctx, "", node, js)
	return err
}

// decodeRecovered runs bd.decode behind diag.Catch and turns a panic in it into
// an error quoting what the parser panicked with: the decoder reads YAML no one
// checked was a schema, and the parser panics on some shapes it accepts
// (docs/parser-library.md#recoverable-panic).
func (bd *Builder) decodeRecovered(ctx context.Context, node *yaml.Node, js *schemaRef) error {
	var err error
	if text, panicked := diag.Catch(func() { err = bd.decode(ctx, node, js) }); panicked {
		return fmt.Errorf("the parser panicked: %s", text)
	}
	return err
}

// Schema records that js was read at documentPointer. The first schema recorded at a
// position is the one held there. A boolean one is noted, as the parser
// decodes some written mappings as false (mend).
func (bd *Builder) Schema(js *schemaRef, documentPointer reftable.DocumentPointer) {
	if js == nil {
		return
	}
	bd.b.at[js] = documentPointer
	if _, held := bd.b.held[documentPointer]; !held {
		bd.b.held[documentPointer] = js
	}
	if js.IsBool() {
		bd.bools = append(bd.bools, recorded{js: js, at: documentPointer})
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
		recorded := bd.bools[len(bd.bools)-1]
		bd.bools = bd.bools[:len(bd.bools)-1]
		node := nodeview.Deref(recorded.js.GetCore().GetRootNode())
		if node == nil || node.Kind != yaml.MappingNode || recorded.mends >= MaxMends {
			continue
		}
		fresh := &schemaRef{}
		if err := bd.decodeRecovered(ctx, node, fresh); err != nil || fresh.IsBool() {
			continue
		}
		*recorded.js = *fresh
		below := len(bd.bools)
		bd.Walk(ctx, recorded.js, recorded.at)
		for i := below; i < len(bd.bools); i++ {
			bd.bools[i].mends = recorded.mends + 1
		}
	}
}

// Object records that model, a reference-or-inline wrapper of kind, was read
// at documentPointer. The first recorded of a kind at a position is the one held there.
func (bd *Builder) Object(model any, kind reftable.RefKind, documentPointer reftable.DocumentPointer) {
	if model == nil || kind == 0 {
		return
	}
	objectSiteValue := objectSite{at: documentPointer, kind: kind}
	bd.b.objectSites[model] = objectSiteValue
	if _, held := bd.b.objects[objectSiteValue]; !held {
		bd.b.objects[objectSiteValue] = model
	}
}

// HoldsObject reports whether a wrapper of kind is held at documentPointer.
func (bd *Builder) HoldsObject(documentPointer reftable.DocumentPointer, kind reftable.RefKind) bool {
	_, held := bd.b.objects[objectSite{at: documentPointer, kind: kind}]
	return held
}

// Walk records js, read at documentPointer, and every schema below it, at the position each
// is read at, until ctx is done: the library's walk reads no
// context (docs/parser-library.md#context-unread), so Walk asks it before each
// item.
func (bd *Builder) Walk(ctx context.Context, js *schemaRef, documentPointer reftable.DocumentPointer) {
	for item := range oas3.Walk(ctx, js) {
		if ctx.Err() != nil {
			return
		}
		at := reftable.DocumentPointer{Doc: documentPointer.Doc, Pointer: documentPointer.Pointer + PointerOf(item.Location)}
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(oas3.SchemaMatcher{
			Schema: func(s *schemaRef) error { bd.Schema(s, at); return nil },
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
	Landing reftable.Landing
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
	for _, landing := range landings {
		if ctx.Err() != nil {
			break
		}
		out = append(out, bd.build(ctx, landing))
	}
	bd.mend(ctx)
	return out
}

// build builds the schema at landing, unless one is held there already: one inside
// a landing built before it, or inside an object built for an object landing.
func (bd *Builder) build(ctx context.Context, landing reftable.Landing) Built {
	built := Built{Landing: landing, Schema: bd.b.held[landing.At]}
	if built.Schema != nil {
		return built
	}
	doc := bd.b.table.Document(landing.At.Doc)
	path, complete := bd.view.DocumentPath(doc.Root, landing.At.Pointer)
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
	bd.Walk(ctx, js, landing.At)
	return built
}

// Bindings returns what bd has built. bd is not used after.
func (bd *Builder) Bindings() *Bindings { return bd.b }
