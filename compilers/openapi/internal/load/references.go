// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// resolvable is a reference object the parser's resolver follows: each
// Referenced* model, whose cycles it reports itself
// (docs/parser-library.md#reference-object-cycle). They share one options
// type, so one call resolves any of them. A schema's $ref is none:
// GetObjectAny is what a schema lacks, so the compiler binds those itself
// (bindSchemas), since the resolver overflows on a loop of them
// (docs/parser-library.md#schema-cycle-overflow).
type resolvable interface {
	IsReference() bool
	IsResolved() bool
	GetReference() references.Reference
	GetObjectAny() any
	Resolve(ctx context.Context, opts references.ResolveOptions) ([]error, error)
}

// resolveWith resolves every reference object in doc, reading external
// documents through reader when one is given, and every reference object in
// what those reach in other documents (GitHub #751). It reports each failure at
// the $ref that failed (GitHub #385), once the pass's diags are asked for. It
// runs ResolveAllReferences' own walk, since that call names no reference.
func resolveWith(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, media MediaTypes, self sourceDocument,
	opts Options, reader *external,
) *resolution {
	p := newResolution(ctx, doc, sibs, media, self, opts, reader)
	p.run(ctx, doc)
	return p
}

// run is resolveWith's work over doc, the document p was made for: the
// source's reference objects, its media type references last, once the walk
// has held every media type they may name (holdMediaType), then those of each
// object they reached in another document, in the order reached (see
// elsewhere). A media type reference names only a media type, so no other
// kind's chain passes one, and the order among them is the walk's.
func (p *resolution) run(ctx context.Context, doc *soa.OpenAPI) {
	var media []visitedRef
	site, err := eachModel(walkSource(ctx, doc, p.sibs, p.media), resolverPanics, func(site jsontext.Pointer, model any) error {
		at := refSite{pointer: site}
		switch m := model.(type) {
		case mediaFault:
			p.faulted(at, m)
		case *refscope.ReferencedMediaType:
			if !m.IsReference() {
				p.holdMediaType(doc, site, m)
				return nil
			}
			media = append(media, visitedRef{site: at, r: m})
		case resolvable:
			if m.IsReference() {
				p.visit(ctx, at, m, p.opts)
			}
		}
		return nil
	})
	p.fail(refSite{pointer: site}, err)
	for i := 0; i < len(media) && ctx.Err() == nil; i++ {
		p.visit(ctx, media[i].site, media[i].r, p.opts)
	}
	for e, ok := p.away.next(); ok && ctx.Err() == nil; e, ok = p.away.next() {
		p.fail(e.at, eachReferenceIn(ctx, e, p.media, func(site refSite, r resolvable) { p.visit(ctx, site, r, e.opts) },
			p.faulted))
	}
}

// faulted reports f, a content entry the parser faulted building a reference
// from, at site, the first place a walk met its mapping, and nowhere else.
func (p *resolution) faulted(site refSite, f mediaFault) {
	if p.reportedFaults[f.node] {
		return
	}
	p.reportedFaults[f.node] = true
	p.failures[site.doc] = append(p.failures[site.doc], diag.Newf(ir.SeverityError, diag.Validation,
		pointerOnly(site.pointer), "this media type's $ref could not be read: %s", f.err.Error()))
}

// holdMediaType stores model, the model of a Media Type Object the source writes
// at site that is no reference, where the resolver looks for what a $ref of the
// source names before it reads the model: read in the model, the object there
// is the library's own, which the resolver cannot take for a media type
// reference (docs/parser-library.md#media-type-references). The key is the
// resolver's own, the base it reads an internal $ref against and the pointer
// it decodes, as external.holdObject keys what it holds.
func (p *resolution) holdMediaType(doc *soa.OpenAPI, site jsontext.Pointer, model *refscope.ReferencedMediaType) {
	if p.internal == "" {
		return
	}
	doc.InitCache()
	doc.StoreReferencedObjectInCache(p.internal+"#"+string(site), model)
}

// diags returns what the pass found, the source's failures first and then each
// other document's, by its name, each placed by place: in the source, or in the
// Document.Sources entry of the document writing the reference, which is
// listed only once every document is read. Two objects the library built for
// one position fail alike there, a finding the compile keeps once.
func (p *resolution) diags(place func(refSite) ir.Provenance) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, doc := range slices.Sorted(maps.Keys(p.failures)) {
		for _, d := range p.failures[doc] {
			d.Provenance = place(refSite{doc: doc, pointer: d.Provenance.Pointer})
			out = append(out, d)
		}
	}
	return out
}

// ends returns where the chain of each reference object the pass visited
// ended, by its site, and holds none for one whose chain stopped, or was cut.
// Each carries the node its chain reached, by which the binder places it
// (docs/parser-library.md#trailing-empty-token).
func (p *resolution) ends() map[refSite]referenceEnd {
	ends := map[refSite]referenceEnd{}
	for _, v := range p.visited {
		c := resolutionChain(v.r)
		if c.cut || c.stopped != "" || len(c.records) == 0 {
			continue
		}
		last := c.records[len(c.records)-1]
		name := p.nameOf(last.path)
		if last.inSource() {
			name = p.selfPath
		}
		ends[v.site] = referenceEnd{name: name, pointer: last.pointer, object: last.object, node: last.node,
			walk: last.walk}
	}
	return ends
}

// elsewhere returns each reference object the pass visited in another
// document, with where a finding at a pointer of that document is placed.
func (p *resolution) elsewhere(place func(refSite) ir.Provenance) []awayRef {
	var out []awayRef
	for _, v := range p.visited {
		if v.site.inSource() {
			continue
		}
		doc := v.site.doc
		out = append(out, awayRef{visitedRef: v,
			place: func(pointer jsontext.Pointer) ir.Provenance { return place(refSite{doc: doc, pointer: pointer}) }})
	}
	return out
}

// used returns every document each reference object the pass visited read, at
// every hop of its resolution, in the order visited, with the site writing it:
// those the source writes, and those written in other documents, which no walk
// of the source shows (see elsewhere).
func (p *resolution) used() []usedDocument {
	var used []usedDocument
	for _, v := range p.visited {
		for _, key := range resolutionTrail(v.r).docs {
			used = append(used, usedDocument{key: key, site: v.site})
		}
	}
	return used
}

// awayRef is a reference object written in another document, and where a
// finding at a pointer of that document is placed.
type awayRef struct {
	visitedRef
	place func(jsontext.Pointer) ir.Provenance
}

// pointerOnly is a Provenance holding pointer and naming no document yet.
func pointerOnly(pointer jsontext.Pointer) ir.Provenance {
	return ir.Provenance{Pointer: pointer}
}

// visitedRef is a reference object a pass visited, and the site writing it.
type visitedRef struct {
	site refSite
	r    resolvable
}

// resolution is one resolver pass over a document: how each reference is
// resolved, and what the pass has found so far.
type resolution struct {
	opts   references.ResolveOptions
	reader *external
	// failures holds the failures at references each document writes, by its
	// name, the source's under "", each at the pointer its Provenance holds
	// (pointerOnly), placed once the documents are listed (see diags).
	failures map[string][]ir.Diagnostic
	// away is what the pass has still to resolve in other documents, and
	// visited every reference object it resolved, in either.
	away    *elsewhere
	visited []visitedRef
	// selfPath and nameOf name the source, and any document, as bind names
	// them (sourceDocument.nameOf).
	selfPath string
	nameOf   func(string) string
	// sibs is what the source writes beside its path items' $refs, walked
	// after doc as the rest of the source, and media the media types the
	// parser does not model, walked with it (walkSource).
	sibs  Siblings
	media MediaTypes
	// esc holds the $refs rewritten for the resolver to read, which a message
	// quotes as the source writes them (GitHub #911).
	esc *escapes
	// internal is the document an internal $ref of the source resolves
	// against, as the resolver names it (internalBase).
	internal string
	// reportedFaults holds each content entry whose fault the pass reported.
	reportedFaults map[*yaml.Node]bool
}

// newResolution returns the pass over doc, the model of self, holding self
// where the resolver looks for it when reader reads other documents.
func newResolution(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, media MediaTypes,
	self sourceDocument, opts Options, reader *external,
) *resolution {
	resolveOpts := references.ResolveOptions{
		TargetLocation:      self.path,
		RootDocument:        doc,
		DisableExternalRefs: opts.Reader == nil,
	}
	if reader != nil {
		resolveOpts.VirtualFS = *reader
		resolveOpts.HTTPClient = *reader
		reader.hold(ctx, sibs, media)
	}
	return &resolution{opts: resolveOpts, reader: reader, sibs: sibs, media: media, esc: self.escapes,
		internal: internalBase(doc, self.path), away: newElsewhere(), reportedFaults: map[*yaml.Node]bool{},
		failures: map[string][]ir.Diagnostic{}, selfPath: self.path, nameOf: self.nameOf}
}

// internalBase is the document an internal $ref of doc, read from path,
// resolves against, as the resolver names it: doc's $self where it writes one,
// which the resolver rebases onto, else path. It is "" where the resolver
// could not name it, so nothing is held under it.
func internalBase(doc *soa.OpenAPI, path string) string {
	base := path
	if self := doc.GetSelf(); self != "" {
		base = self
	}
	abs, err := references.ResolveAbsoluteReference(references.Reference("#"), base)
	if err != nil {
		return ""
	}
	return abs.AbsoluteReference
}

// visit resolves r, the reference written at site, under opts, which read it
// against the document writing it, reports a failure to resolve it, and notes
// where it ended (see elsewhere). A reference an earlier $ref's chain resolved
// is not resolved again. The library counts one resolved once a chain that
// failed passed through it, so which members of a failing chain are reported
// follows the walk's order (GitHub #767). A message quotes the $ref as the
// document writes it. What is wrong with the object a resolution reaches is the
// table's to say (validateReached).
func (p *resolution) visit(ctx context.Context, site refSite, r resolvable, opts references.ResolveOptions) {
	var err error
	if !r.IsResolved() {
		_, err = r.Resolve(ctx, opts)
	}
	if p.reader != nil {
		err = p.reader.settle(ctx, r, opts, err)
	}
	c := resolutionChain(r)
	if p.media.adopt(c) {
		c = resolutionChain(r)
	}
	p.away.reach(site, c, opts, p.nameOf)
	p.visited = append(p.visited, visitedRef{site: site, r: r})
	if err == nil {
		return
	}
	// A failed resolution built no object. Where the position its chain ends on
	// holds none of the kind asked for, the failure says so as the binder says
	// it of a schema $ref (GitHub #1044).
	if c.endsOnNothing() {
		err = notObjectError{kind: objectKind(r), through: p.through(c), err: err}
	}
	p.failures[site.doc] = append(p.failures[site.doc],
		failureDiag(pointerOnly(site.pointer), p.esc.writtenOf(r), p.esc.trailOf(c), err))
}

// through quotes the reference written at the hop before c's last, where the
// chain went through another reference to reach its end, with the document
// it is written in when that is not the source; "" where it went through none.
func (p *resolution) through(c chain) string {
	if len(c.records) < 2 {
		return ""
	}
	prev := c.records[len(c.records)-2]
	quoted := strconv.Quote(string(p.esc.writtenOf(prev.object)))
	if prev.inSource() {
		return quoted
	}
	return quoted + " in " + prev.path
}

// objectKind names the kind of object the reference object r resolves to, as
// a message names it.
func objectKind(r resolvable) string {
	switch r.(type) {
	case *soa.ReferencedPathItem:
		return "path item"
	case *soa.ReferencedParameter:
		return "parameter"
	case *soa.ReferencedHeader:
		return "header"
	case *soa.ReferencedRequestBody:
		return "request body"
	case *soa.ReferencedResponse:
		return "response"
	case *soa.ReferencedExample:
		return "example"
	case *soa.ReferencedLink:
		return "link"
	case *soa.ReferencedCallback:
		return "callback"
	case *soa.ReferencedSecurityScheme:
		return "security scheme"
	case *refscope.ReferencedMediaType:
		return "media type"
	default:
		return "object"
	}
}

// notObjectError is why a reference object's resolution failed where the
// position its chain ends on holds no object of kind, reached through the
// reference through quotes, if any. failureDiag words it as the binder words a
// schema $ref to a position holding no schema (bind.NotObject).
type notObjectError struct {
	kind, through string
	err           error
}

// Error returns the failure as the parser reported it.
func (e notObjectError) Error() string { return e.err.Error() }

// Unwrap returns the failure.
func (e notObjectError) Unwrap() error { return e.err }

// fail reports the fault a walk ended on, at the reference it ended at, and
// nothing for no fault.
func (p *resolution) fail(site refSite, err error) {
	if err != nil {
		p.failures[site.doc] = append(p.failures[site.doc],
			diag.Newf(ir.SeverityError, diag.UnresolvedRef, pointerOnly(site.pointer), "%s", err.Error()))
	}
}

// eachReference calls visit with each reference the walk reaches, resolved or
// not, and the pointer that writes it. The walk does not descend into what a
// resolved reference names, so each is visited once, where it is written.
func eachReference(items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, resolvable) error,
) (jsontext.Pointer, error) {
	return eachModel(items, what, func(site jsontext.Pointer, r resolvable) error {
		if !r.IsReference() {
			return nil
		}
		return visit(site, r)
	})
}

// eachModel calls visit with each model of kind T the walk reaches, and the
// pointer that names it.
func eachModel[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, T) error,
) (jsontext.Pointer, error) {
	return eachSighting(items, what, func(site jsontext.Pointer, _ soa.Locations, m T) error {
		return visit(site, m)
	})
}

// eachSighting is eachModel handing visit the location of each model as well.
//
// The walk and visit run behind runRecovered, so a panic in either becomes an
// error naming what was running. It stops the walk at site, the model being
// visited, or the root when the walk panicked. An error from visit stops it too.
func eachSighting[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, soa.Locations, T) error,
) (jsontext.Pointer, error) {
	var site jsontext.Pointer
	err := runRecovered(what, func() error {
		for item := range items {
			if err := item.Match(soa.Matcher{Any: func(model any) error {
				m, ok := model.(T)
				if !ok {
					return nil
				}
				site = jsontext.Pointer(item.Location.ToJSONPointer())
				if err := visit(site, item.Location, m); err != nil {
					return err
				}
				site = ""
				return nil
			}}); err != nil {
				return err
			}
		}
		return nil
	})
	return site, err
}

// resolverPanics names the work of the reference resolver in a panic it
// recovers from.
const resolverPanics = "reference resolver"

// runRecovered runs f behind diag.Catch and returns its error, or, when it
// panicked, an error naming what was running and quoting the panic. The library
// panics on the caller's goroutine on some shapes its parser accepts, such as a
// $ref with no value (docs/parser-library.md#recoverable-panic).
func runRecovered(what string, f func() error) error {
	var err error
	if text, panicked := diag.Catch(func() { err = f() }); panicked {
		return fmt.Errorf("%s panicked: %s", what, text)
	}
	return err
}

// failureDiag reports that the reference ref could not be resolved, quoting it
// as written, with the resolver's reason. When t got past it and stopped at
// another reference, that one is quoted too: the reason is about it, and need
// not fit ref, as "external reference not allowed" does not fit a $ref to
// #/components. A chain ending on no object of its kind is worded as the binder
// words one (notObjectError), quoting the reference it went through, if any.
func failureDiag(site ir.Provenance, ref references.Reference, t trail, err error) ir.Diagnostic {
	reason := err.Error()
	var unparsable *bind.UnparsableError
	var held notObjectError
	switch {
	case errors.As(err, &held) && held.through != "":
		return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q, through %s: %s",
			string(ref), held.through, bind.Hop{Reason: bind.NotObject, Detail: held.kind}.Why())
	case errors.As(err, &held):
		reason = bind.Hop{Reason: bind.NotObject, Detail: held.kind}.Why()
	case errors.As(err, &unparsable):
		reason = bind.Hop{Reason: bind.Unparsable, Detail: unparsable.Err.Error()}.Why()
	case errors.Is(err, bind.ErrEmptyDocument):
		reason = bind.Hop{Reason: bind.Empty}.Why()
	case errors.As(err, new(unread)):
		reason = bind.Hop{Reason: bind.Unreadable, Detail: reason}.Why()
	}
	if len(t.docs) > 0 && t.stopped != "" {
		return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q, through %s: %s",
			string(ref), quotedStop(t), reason)
	}
	return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q: %s",
		string(ref), reason)
}

// unread is why a document the resolver asked Open or Do for was not read.
// failureDiag words it as the binder words its own (bind.Unreadable), so one
// failure reads alike whichever resolved the reference.
type unread struct{ err error }

// Error returns the failure as the read reported it.
func (u unread) Error() string { return u.err.Error() }

// Unwrap returns the failure.
func (u unread) Unwrap() error { return u.err }

// findingPlace names the document a finding made along t is in: the last one t
// read, or, when t stopped at a reference it could not follow, the one that
// reference names, which no record holds. A trail cut at maxResolutionHops, or
// empty because resolutionTrail does not know the kind, names none.
func findingPlace(t trail) string {
	switch {
	case t.stopped != "":
		return "the document that " + quotedStop(t) + " names"
	case t.cut || len(t.docs) == 0:
		return "a document the $ref leads to"
	default:
		return t.docs[len(t.docs)-1]
	}
}

// quotedStop quotes the reference t stopped at, with the document it is written
// in, the last one t read, when that is not the source. Quoted alone, an
// internal reference written in another document would read as the source's.
func quotedStop(t trail) string {
	if len(t.docs) == 0 || t.endsInSource {
		return strconv.Quote(string(t.stopped))
	}
	return strconv.Quote(string(t.stopped)) + " in " + t.docs[len(t.docs)-1]
}
