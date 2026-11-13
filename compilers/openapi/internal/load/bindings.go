// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// bound is what binding the source's references found: the bindings the
// lowering reads, the objects built for the table's object landings, the
// schemas built for its landings, and every document a reference read beside
// the source.
type bound struct {
	bindings *typed.Bindings
	objects  []builtObject
	built    []typed.Built
	faults   mediaFaults
	others   Others
}

// bindAll resolves every $ref of doc, the source's model, in the compiler
// (internal/bind), never through the parser's resolver, which overflows the
// stack on a cycle of schemas (docs/parser-library.md#schema-cycle-overflow)
// and reads a Reference Object otherwise than RFC 6901 and the model do, by
// declaration order (docs/parser-library.md#reference-object-cycle,
// docs/parser-library.md#trailing-empty-token,
// docs/parser-library.md#duplicate-key). It indexes the model's schemas and
// wrappers where the table places them, then builds an object or a schema for
// each landing. reader reads other documents, or is nil; d reads each
// dialect's rows (dialectRuleOf); media yields the media types the parser does
// not model.
func bindAll(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, media MediaTypes, self sourceDocument,
	reader *external, refuse func(uri string) error, d *dialects,
) bound {
	opened := map[string]*yaml.Node{}
	cfg := bind.Config{Self: self.path, Locate: self.locate, Refuse: refuse, Limit: budget.AtMost(bind.MaxSteps),
		Dialect: func(name, dialect string) bind.DialectRule {
			declared := doc.GetOpenAPI()
			if name != self.path {
				declared = declaredVersion(opened[name])
			}
			_, _, rows := d.of(declared, dialect)
			return dialectRuleOf(rows.Schema, dialect)
		}}
	if reader != nil {
		cfg.Open = func(name string) (*yaml.Node, error) {
			tree, err := reader.document(ctx, name)
			opened[name] = tree
			return tree, err
		}
	}
	table := bind.Bind(ctx, self.root, cfg)
	bd := typed.NewBuilder(table)
	faults := mediaFaults{}
	indexWalk(bd, walkSource(ctx, doc, sibs, media), bind.Position{}, faults)
	objects := buildObjects(ctx, bd, table, media, faults)
	return bound{bindings: bd.Bindings(), objects: objects, built: bd.Build(ctx), faults: faults}
}

// indexWalk tells bd where each schema, discriminator and reference-or-inline
// wrapper of items was read: at base, where the walk starts, followed by the
// item's location. It notes in faults where it meets each content entry the
// parser faulted building a reference from.
func indexWalk(bd *typed.Builder, items iter.Seq[soa.WalkItem], base bind.Position, faults mediaFaults) {
	for item := range items {
		at := bind.Position{Doc: base.Doc, Pointer: base.Pointer + typed.PointerOf(item.Location)}
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{
			Schema:        func(js *schemaRef) error { bd.Schema(js, at); return nil },
			Discriminator: func(d *oasDiscriminator) error { bd.Discriminator(d, at); return nil },
			Any: func(model any) error {
				if kind, ok := refKindOf(model); ok {
					bd.Object(model, kind, at)
				}
				if f, ok := model.(mediaFault); ok {
					faults.note(at, f)
				}
				return nil
			},
		})
	}
}

// mediaFaults is each content entry the parser faulted building a reference
// from, by the mapping it is written as, with the first position a walk met it
// at, where it is reported once.
type mediaFaults map[*yaml.Node]placedFault

// placedFault is a fault, and where it is reported.
type placedFault struct {
	at  bind.Position
	err error
}

// note records f at at, unless a walk met its mapping before.
func (m mediaFaults) note(at bind.Position, f mediaFault) {
	if _, met := m[f.node]; !met {
		m[f.node] = placedFault{at: at, err: f.err}
	}
}

// diags reports each fault once, where it was first met, in order of position.
func (m mediaFaults) diags(sites places, table *bind.Table) []ir.Diagnostic {
	order := bind.PositionOrder(table)
	faults := slices.SortedFunc(maps.Values(m), func(x, y placedFault) int { return order(x.at, y.at) })
	diags := make([]ir.Diagnostic, 0, len(faults))
	for _, f := range faults {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, sites.position(f.at),
			"this media type's $ref could not be read: %s", f.err.Error()))
	}
	return diags
}

// locate is bind.Config.Locate for the source: the document uri names when
// written in the one named base (resolve.Locate), named as bind names the
// source when it is the source.
func (s sourceDocument) locate(uri, base string) (string, bool) {
	named, ok := refscope.Locate(uri, base)
	if !ok {
		return "", false
	}
	return s.nameOf(named), true
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

// bindingDiags reports what the table found: each $ref whose own hop fails, a
// schema's or a Reference Object's, at the $ref, in whichever document, so a
// chain failing past its first hop is reported once, where it fails; each loop
// of $refs once, where the table places it; and the budget or the merge
// bound, if either stopped the reading.
func bindingDiags(sites places, table *bind.Table) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range table.Entries() {
		if e.Hop.Status == bind.Unresolved {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, sites.position(e.Site),
				"unresolved $ref %q: %s", e.Hop.Ref, e.Hop.Why()))
		}
	}
	for _, r := range table.References() {
		if r.Hop.Status == bind.Unresolved {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, sites.position(r.Site),
				"unresolved $ref %q: %s", r.Hop.Ref, r.Hop.Why()))
		}
	}
	for _, l := range table.Loops() {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.CyclicRef, sites.position(l.Site),
			"cyclic $ref: the chain of $refs through %s comes back to where it started and never reaches %s",
			spellLoop(l, table), reachesNone(l.Kind)))
	}
	at := sites.position(bind.Position{})
	if table.Exhausted() {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, at,
			"resolving the $refs takes more than %d steps; the $refs past them are left unresolved",
			table.Limit().Cap()))
	}
	if table.Truncated() {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at,
			"a mapping merges through `<<` deeper than %d levels, which resolving the $refs does not "+
				"expand; a $ref past that, or one that reads a key past it, is left unresolved, and where that "+
				"mapping is a schema, no $dynamicRef in its document is expanded", nodeview.MergeDepthLimit))
	}
	return diags
}

// reachesNone names what a loop of $refs read as kind never reaches: a schema,
// for a loop of schema $refs, or an object of that kind.
func reachesNone(kind bind.RefKind) string {
	if kind == 0 {
		return "a schema"
	}
	return "a " + kind.String()
}

// buildFailures reports each landing no schema could be built for, and each
// object landing no object could, at the least position whose chain reaches
// it, in whichever document, with why.
func buildFailures(sites places, built []typed.Built, objects []builtObject) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, b := range built {
		if b.Err != nil && b.Landing.Reached {
			diags = append(diags, buildFailure(sites, b.Landing.At, b.Landing.Site, b.Landing.Name, "schema", b.Err))
		}
	}
	for _, o := range objects {
		if o.err != nil && o.landing.Reached {
			diags = append(diags, buildFailure(sites, o.landing.At, o.landing.Site, o.landing.Name,
				o.landing.Kind.String(), o.err))
		}
	}
	return diags
}

// buildFailure reports, at site, that no object of kind could be built at at,
// in the document named name, with why.
func buildFailure(sites places, at, site bind.Position, name, kind string, err error) ir.Diagnostic {
	where := "#" + string(at.Pointer)
	if at.Doc != site.Doc {
		where += " in " + name
	}
	return diag.Newf(ir.SeverityError, diag.Validation, sites.position(site),
		"the $ref here reaches %s, where no %s could be built: %s", where, kind, err.Error())
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

// document is bind.Config.Open: the tree of the document named name, read
// once in the compile, under its budgets (see externalReads.once), and
// prepared as the source is. A name that is a URL (refscope.IsURL) is keyed as
// a request spells it and read at that key; any other is read as the file it
// names (see fileNamed).
func (e external) document(ctx context.Context, name string) (*yaml.Node, error) {
	var a answer
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, name, nil); err == nil && refscope.IsURL(name) {
		key := req.URL.String()
		a = e.readAs(ctx, documentID(key), key, key)
	} else {
		a = e.readAs(ctx, fileNamed(name), name, fileNamed(name))
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
