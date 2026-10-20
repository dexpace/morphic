package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// bound is what binding the source's schema references found: the bindings
// the lowering reads, the schemas built for the table's landings, and what it
// reports. Until build runs, it holds the builder that builds them.
type bound struct {
	builder  *typed.Builder
	bindings *typed.Bindings
	built    []typed.Built
	diags    []ir.Diagnostic
}

// build builds the schemas for the table's landings, from the trees as the
// parser was handed them (see resolve), and finishes the bindings.
func (b *bound) build(ctx context.Context) {
	if b.builder == nil {
		return
	}
	b.built = b.builder.Build(ctx)
	b.bindings, b.builder = b.builder.Bindings(), nil
}

// bindSchemas resolves every schema $ref of doc, the source's model, in the
// compiler (internal/bind), never through the parser's resolver, which
// overflows the stack on a cycle (docs/parser-library.md#schema-cycle-overflow)
// and answers by declaration order (docs/parser-library.md#defs-order). It runs
// once every reference object is resolved, since a pointer through one reads
// on where the parser resolved it. reader reads other documents, or is nil
// when none is read.
func bindSchemas(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI,
	sibs Siblings, self sourceDocument, reader *external,
) bound {
	ends := referenceEnds(ctx, doc, sibs, self)
	cfg := bind.Config{Self: self.path, Locate: self.locate, Beside: sibs.Beside, Written: self.escapes.original,
		Through: func(p jsontext.Pointer) (bind.ReferenceEnd, bool) {
			e, ok := ends[p]
			return bind.ReferenceEnd{Name: e.name, Pointer: e.pointer, Node: e.node}, ok
		}}
	if reader != nil {
		cfg.Open = func(name string) (*yaml.Node, error) { return reader.document(ctx, name) }
	}
	table := bind.Bind(self.root, cfg)
	bd := typed.NewBuilder(table)
	indexSource(ctx, bd, doc, sibs)
	indexEnds(ctx, bd, table, ends)
	return bound{builder: bd, diags: bindingDiags(at, table)}
}

// referenceEnd is where the chain of one of the source's reference objects
// ended, as the parser resolved it: the document, by the name bind reads it
// under, and the pointer there, and the object built there, with its node and
// its walk.
type referenceEnd struct {
	name    string
	pointer jsontext.Pointer
	object  any
	node    *yaml.Node
	walk    func(context.Context) iter.Seq[soa.WalkItem]
}

// referenceEnds returns where each of the source's reference objects ended,
// by the pointer writing it, and holds none for one whose chain stopped, or
// was cut. Each carries the node its chain reached, by which the binder places
// it (docs/parser-library.md#trailing-empty-token).
func referenceEnds(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, self sourceDocument,
) map[jsontext.Pointer]referenceEnd {
	ends := map[jsontext.Pointer]referenceEnd{}
	// A walk that panics stops here; what it read is kept, and resolution has
	// reported the panic at the reference it stopped at.
	_, _ = eachReference(walkSource(ctx, doc, sibs), resolverPanics, func(site jsontext.Pointer, r resolvable) error {
		c := resolutionChain(r)
		if c.cut || c.stopped != "" || len(c.records) == 0 {
			return nil
		}
		last := c.records[len(c.records)-1]
		ends[site] = referenceEnd{name: self.nameOf(last.path), pointer: last.pointer, object: last.object,
			node: last.node, walk: last.walk}
		return nil
	})
	return ends
}

// indexSource tells bd where each schema and discriminator of the source's
// model was read.
func indexSource(ctx context.Context, bd *typed.Builder, doc *soa.OpenAPI, sibs Siblings) {
	for item := range walkSource(ctx, doc, sibs) {
		at := bind.Position{Pointer: typed.PointerOf(item.Location)}
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{
			Schema:        func(js *schemaRef) error { bd.Schema(js, at); return nil },
			Discriminator: func(d *oasDiscriminator) error { bd.Discriminator(d, at); return nil },
		})
	}
}

// indexEnds tells bd where each schema and discriminator of what a reference
// object resolved to was read: where the table reads its end (bind.Table.Through),
// which the lowering reads through that reference. An object many references
// resolved to is walked once, at the least of them, so that which it is held
// at never follows the order of a map.
func indexEnds(ctx context.Context, bd *typed.Builder, table *bind.Table, ends map[jsontext.Pointer]referenceEnd) {
	walked := map[any]bool{}
	for _, site := range slices.Sorted(maps.Keys(ends)) {
		e := ends[site]
		end, ok := table.Through(site)
		if !ok || walked[e.object] {
			continue
		}
		walked[e.object] = true
		for item := range e.walk(ctx) {
			at := bind.Position{Doc: end.Doc, Pointer: end.Pointer + typed.PointerOf(item.Location)}
			// Match returns only what its matcher does, and this one never fails.
			_ = item.Match(soa.Matcher{
				Schema:        func(js *schemaRef) error { bd.Schema(js, at); return nil },
				Discriminator: func(d *oasDiscriminator) error { bd.Discriminator(d, at); return nil },
			})
		}
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

// bindingDiags reports what the table found in the source: each $ref that
// fails, at the $ref (refDiags); each loop of $refs once, where the table
// places it; and the budget or the merge bound, if either stopped the reading.
func bindingDiags(at func(jsontext.Pointer) ir.Provenance, table *bind.Table) []ir.Diagnostic {
	diags := refDiags(at, table)
	for _, l := range table.Loops() {
		if l.Reported {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.CyclicRef, at(l.Site.Pointer),
				"cyclic $ref: the chain of $refs through %s comes back to where it started and never reaches a schema",
				spellLoop(l, table)))
		}
	}
	if table.Exhausted() {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, at(""),
			"resolving the schema $refs takes more than %d steps; the $refs past them are left unresolved",
			bind.DefaultLimit))
	}
	if table.Truncated() {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at(""),
			"a mapping merges through `<<` deeper than %d levels, which resolving the schema $refs does not "+
				"expand; a $ref past that, or one that reads a key past it, is left unresolved", nodeview.MergeDepthLimit))
	}
	return diags
}

// refDiags reports each source $ref whose own hop fails, and each whose hop
// leaves the source for content whose chain fails there, which no source
// position is written in. A $ref whose hop lands on another source $ref is
// not reported for that one's failure: that one is.
func refDiags(at func(jsontext.Pointer) ir.Provenance, table *bind.Table) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range table.Entries() {
		switch {
		case e.Site.Doc != bind.Source:
		case e.Hop.Status == bind.Unresolved:
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(e.Site.Pointer),
				"unresolved $ref %q: %s", e.Hop.Ref, e.Hop.Why()))
		case e.Hop.Target.Doc != bind.Source && e.Chain.Status == bind.Unresolved && e.Chain.Failed.Doc != bind.Source:
			failed, _ := table.Entry(e.Chain.Failed)
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(e.Site.Pointer),
				"unresolved $ref %q: its chain reaches #%s in %s, whose $ref %q is unresolved: %s", e.Hop.Ref,
				failed.Site.Pointer, table.Document(failed.Site.Doc).Name, failed.Hop.Ref, failed.Hop.Why()))
		}
	}
	return diags
}

// buildFailures reports each landing no schema could be built for, at the
// least source position whose chain reaches it, with why. One only another
// document's content reaches is reported where the lowering meets its $ref.
func buildFailures(at func(jsontext.Pointer) ir.Provenance, built []typed.Built) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, b := range built {
		if b.Err == nil || !b.Landing.Reached {
			continue
		}
		where := "#" + string(b.Landing.At.Pointer)
		if b.Landing.At.Doc != bind.Source {
			where += " in " + b.Landing.Name
		}
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(b.Landing.Site.Pointer),
			"the $ref here reaches %s, where no schema could be built: %s", where, b.Err.Error()))
	}
	return diags
}

// spellLoop names l's members, in order: a source position by its pointer,
// and another document's by its pointer in that document's name, as refDiags
// names one.
func spellLoop(l bind.Loop, table *bind.Table) string {
	parts := make([]string, len(l.Members))
	for i, m := range l.Members {
		parts[i] = "#" + string(m.Pointer)
		if m.Doc != bind.Source {
			parts[i] += " in " + table.Document(m.Doc).Name
		}
	}
	return strings.Join(parts, ", ")
}

// document is bind.Config.Open: the tree of the document named name, read as
// the parser's resolver reads it, once in the compile, under its budgets (see
// externalReads.once), and prepared as every document it reads is. A name the
// resolver classifies as a URL is fetched by the request built from it, keyed
// as the resolver keys it; any other is read as a file.
func (e external) document(ctx context.Context, name string) (*yaml.Node, error) {
	var a answer
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, name, nil); err == nil && refscope.IsURL(name) {
		key := req.URL.String()
		a = e.read.once(key, e.opts.MaxExternalDocuments, func() answer { return e.fetch(req, key) })
		if a.err == nil && (a.status < http.StatusOK || a.status >= http.StatusMultipleChoices) {
			return nil, fmt.Errorf("fetching it answered %d", a.status)
		}
	} else {
		a = e.read.once(name, e.opts.MaxExternalDocuments, func() answer { return e.readFile(name, name) })
	}
	if a.err != nil {
		return nil, a.err
	}
	tree, ok := e.read.preparedFor(name)
	if !ok {
		return nil, errors.New("it does not parse as YAML")
	}
	return tree, nil
}
