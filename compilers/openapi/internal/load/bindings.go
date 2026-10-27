package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// bound is what binding the source's schema references found: the bindings
// the lowering reads, the schemas built for the table's landings, and every
// document a reference read beside the source.
type bound struct {
	bindings *typed.Bindings
	built    []typed.Built
	others   Others
}

// placer returns where a finding at a position of b's table is placed: at's
// Provenance for the source's, and for another document's its own
// Document.Sources entry. One no entry holds, which a document the binder read
// always has, is placed at the source's root rather than nowhere.
func (b bound) placer(at func(jsontext.Pointer) ir.Provenance) func(bind.Position) ir.Provenance {
	table := b.bindings.Table()
	return func(p bind.Position) ir.Provenance {
		if p.Doc == bind.Source {
			return at(p.Pointer)
		}
		o, ok := b.others.Named(table.Document(p.Doc).Name)
		if !ok {
			return at("")
		}
		return ir.Provenance{Source: o.Index, Pointer: p.Pointer}
	}
}

// bindSchemas resolves every schema $ref of doc, the source's model, in the
// compiler (internal/bind), never through the parser's resolver, which
// overflows the stack on a cycle (docs/parser-library.md#schema-cycle-overflow)
// and answers by declaration order (docs/parser-library.md#defs-order), and
// builds a schema for each landing from the trees as the parser was handed
// them (see resolve). It runs once every reference object is resolved, since a
// pointer through one reads on where the parser resolved it: ends says where
// each one's chain ended, in whichever document writes it
// (resolution.ends). reader reads other documents, or is nil when none is read.
func bindSchemas(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, self sourceDocument, reader *external,
	ends map[refSite]referenceEnd,
) bound {
	cfg := bind.Config{Self: self.path, Locate: self.locate, Beside: sibs.Beside, Written: self.escapes.original,
		Refuse: self.escapes.refusal(), Limit: budget.AtMost(bind.MaxSteps),
		Through: func(name string, p jsontext.Pointer) (bind.ReferenceEnd, bool) {
			e, ok := ends[self.siteIn(name, p)]
			return bind.ReferenceEnd{Name: e.name, Pointer: e.pointer, Node: e.node}, ok
		}}
	if reader != nil {
		cfg.Open = func(name string) (*yaml.Node, error) { return reader.document(ctx, name) }
	}
	table := bind.Bind(ctx, self.root, cfg)
	bd := typed.NewBuilder(table)
	indexWalk(bd, walkSource(ctx, doc, sibs), bind.Position{})
	indexEnds(ctx, bd, table, ends, self)
	return bound{bindings: bd.Bindings(), built: bd.Build(ctx)}
}

// siteIn is the site of the reference at pointer in the document bind names
// name: the source's, or another's, keyed as refSite keys it.
func (s sourceDocument) siteIn(name string, pointer jsontext.Pointer) refSite {
	if name == s.path {
		return refSite{pointer: pointer}
	}
	return refSite{doc: readKey(name), pointer: pointer}
}

// positionOf is the position of site in table, whose documents are matched as
// refSite keys them, and false for a site in a document the table did not read.
func (s sourceDocument) positionOf(table *bind.Table, site refSite) (bind.Position, bool) {
	if site.inSource() {
		return bind.Position{Doc: bind.Source, Pointer: site.pointer}, true
	}
	for id := range bind.DocID(table.Documents()) {
		if readKey(table.Document(id).Name) == readKey(site.doc) {
			return bind.Position{Doc: id, Pointer: site.pointer}, true
		}
	}
	return bind.Position{}, false
}

// referenceEnd is where the chain of a reference object ended, as the parser
// resolved it: the document, by the name bind reads it under, and the pointer
// there, and the object built there, with its node and its walk.
type referenceEnd struct {
	name    string
	pointer jsontext.Pointer
	object  any
	node    *yaml.Node
	walk    func(context.Context) iter.Seq[soa.WalkItem]
}

// indexWalk tells bd where each schema and discriminator of items was read:
// at base, where the walk starts, followed by the item's location.
func indexWalk(bd *typed.Builder, items iter.Seq[soa.WalkItem], base bind.Position) {
	for item := range items {
		at := bind.Position{Doc: base.Doc, Pointer: base.Pointer + typed.PointerOf(item.Location)}
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{
			Schema:        func(js *schemaRef) error { bd.Schema(js, at); return nil },
			Discriminator: func(d *oasDiscriminator) error { bd.Discriminator(d, at); return nil },
		})
	}
}

// indexEnds tells bd where each schema and discriminator of what a reference
// object resolved to was read: where the table reads its end (bind.Table.Through),
// which the lowering reads through that reference, in whichever document
// writes it. An object many references resolved to is walked once, at the
// least of them the table places (compareSites), so that which it is held at
// never follows the order of a map. One the table places at none is recorded
// as unplaced, under the least of them.
func indexEnds(ctx context.Context, bd *typed.Builder, table *bind.Table, ends map[refSite]referenceEnd,
	self sourceDocument,
) {
	sites := slices.SortedFunc(maps.Keys(ends), compareSites)
	walked := map[any]bool{}
	for _, site := range sites {
		if ctx.Err() != nil {
			return
		}
		e := ends[site]
		at, placed := self.positionOf(table, site)
		end, ok := table.Through(at)
		if !placed || !ok || walked[e.object] {
			continue
		}
		walked[e.object] = true
		indexWalk(bd, e.walk(ctx), end)
	}
	for _, site := range sites {
		if ctx.Err() != nil {
			return
		}
		if e := ends[site]; !walked[e.object] {
			walked[e.object] = true
			unplaced(ctx, bd, e, site)
		}
	}
}

// unplaced records each schema and discriminator of e, the end of the
// reference object at site, as unplaced: the table could not read where the
// parser resolved it, so the lowering reports each $ref in it with why, naming
// site, and its document when that is not the source.
func unplaced(ctx context.Context, bd *typed.Builder, e referenceEnd, site refSite) {
	spelled := string(site.pointer)
	if !site.inSource() {
		spelled += " in " + site.doc
	}
	for item := range e.walk(ctx) {
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{
			Schema:        func(js *schemaRef) error { bd.Unplaced(js, spelled); return nil },
			Discriminator: func(d *oasDiscriminator) error { bd.Unplaced(d, spelled); return nil },
		})
	}
}

// locate is bind.Config.Locate for the source: the document uri names when
// written in the one named base, as the parser's resolver places it, named as
// bind names the source when it is the source.
func (s sourceDocument) locate(uri, base string) (string, bool) {
	abs, err := references.ResolveAbsoluteReference(references.Reference(uri), base)
	if err != nil {
		return "", false
	}
	return s.nameOf(abs.AbsoluteReference), true
}

// nameOf is the name bind reads the document name names under: the source's
// path for the source, however it is spelled (refscope.SameDocument), and name
// itself for any other.
func (s sourceDocument) nameOf(name string) string {
	if refscope.SameDocument(s.path, name) {
		return s.path
	}
	return name
}

// bindingDiags reports what the table found: each $ref that fails, at the
// $ref, in whichever document; each loop of $refs once, where the table
// places it; and the budget or the merge bound, if either stopped the reading.
func bindingDiags(place func(bind.Position) ir.Provenance, table *bind.Table) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range table.Entries() {
		if e.Hop.Status == bind.Unresolved {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, place(e.Site),
				"unresolved $ref %q: %s", e.Hop.Ref, e.Hop.Why()))
		}
	}
	for _, l := range table.Loops() {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.CyclicRef, place(l.Site),
			"cyclic $ref: the chain of $refs through %s comes back to where it started and never reaches a schema",
			spellLoop(l, table)))
	}
	at := place(bind.Position{})
	if table.Exhausted() {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, at,
			"resolving the schema $refs takes more than %d steps; the $refs past them are left unresolved",
			table.Limit().Cap()))
	}
	if table.Truncated() {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at,
			"a mapping merges through `<<` deeper than %d levels, which resolving the schema $refs does not "+
				"expand; a $ref past that, or one that reads a key past it, is left unresolved", nodeview.MergeDepthLimit))
	}
	return diags
}

// buildFailures reports each landing no schema could be built for, at the
// least position whose chain reaches it, in whichever document, with why.
func buildFailures(place func(bind.Position) ir.Provenance, built []typed.Built) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, b := range built {
		if b.Err == nil || !b.Landing.Reached {
			continue
		}
		where := "#" + string(b.Landing.At.Pointer)
		if b.Landing.At.Doc != b.Landing.Site.Doc {
			where += " in " + b.Landing.Name
		}
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, place(b.Landing.Site),
			"the $ref here reaches %s, where no schema could be built: %s", where, b.Err.Error()))
	}
	return diags
}

// spellLoop names l's members, in order: one in the document l is reported in
// by its pointer, and another document's by its pointer in that document's
// name.
func spellLoop(l bind.Loop, table *bind.Table) string {
	parts := make([]string, len(l.Members))
	for i, m := range l.Members {
		parts[i] = "#" + string(m.Pointer)
		if m.Doc != l.Site.Doc {
			parts[i] += " in " + table.Document(m.Doc).Name
		}
	}
	return strings.Join(parts, ", ")
}

// document is bind.Config.Open: the tree of the document named name, read as
// the parser's resolver reads it, once in the compile, under its budgets (see
// externalReads.once), and prepared as every document it reads is. A name the
// resolver classifies as a URL is keyed as the resolver keys it and read at
// that key; any other is read as the file it names (see fileNamed).
func (e external) document(ctx context.Context, name string) (*yaml.Node, error) {
	var a answer
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, name, nil); err == nil && refscope.IsURL(name) {
		key := req.URL.String()
		a = e.read.once(key, func() answer { return e.fetch(ctx, key, key) })
	} else {
		a = e.read.once(name, func() answer { return e.fetch(ctx, name, fileNamed(name)) })
	}
	if a.err != nil {
		return nil, a.err
	}
	tree, ok := e.read.preparedFor(name)
	if !ok {
		return nil, fmt.Errorf("internal: no tree was prepared for %s", name)
	}
	return tree, nil
}
