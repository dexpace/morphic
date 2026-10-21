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
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
// the $ref that failed (GitHub #385), and each finding in what a reference
// names where reachedFinding places it (GitHub #537), once the pass's diags
// are asked for. It runs ResolveAllReferences' own walk, since that call names
// no reference for either. A finding is reported once (see reachedFindings),
// and not where the source's own validation reported it.
func resolveWith(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, self sourceDocument, opts Options,
	reader *external,
) *resolution {
	p := newResolution(ctx, doc, sibs, self, opts, reader)
	p.run(ctx, doc)
	return p
}

// run is resolveWith's work over doc, the document p was made for: the
// source's reference objects, then those of each object they reached in
// another document, in the order reached (see elsewhere).
func (p *resolution) run(ctx context.Context, doc *soa.OpenAPI) {
	site, err := eachReference(walkSource(ctx, doc, p.sibs), resolverPanics, func(site jsontext.Pointer, r resolvable) error {
		p.visit(ctx, refSite{pointer: site}, r, p.opts)
		return nil
	})
	p.fail(refSite{pointer: site}, err)
	for e, ok := p.away.next(); ok; e, ok = p.away.next() {
		p.fail(e.at, eachReferenceIn(ctx, e, func(site refSite, r resolvable) { p.visit(ctx, site, r, e.opts) }))
	}
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
	return append(out, p.found.diags(place)...)
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
	found    reachedFindings
	// away is what the pass has still to resolve in other documents, and
	// visited every reference object it resolved, in either.
	away    *elsewhere
	visited []visitedRef
	// selfPath and nameOf name the source, and any document, as bind names
	// them (sourceDocument.nameOf).
	selfPath string
	nameOf   func(string) string
	// sibs is what the source writes beside its path items' $refs, walked
	// after doc as the rest of the source.
	sibs Siblings
	// esc holds the $refs rewritten for the resolver to read, which a message
	// quotes as the source writes them (GitHub #911).
	esc *escapes
	// version and validateOpts are the source's declared version and what its
	// validation hands each object, which a resolution's findings are judged by
	// (unrequired), in another document as well, as a reached schema's are
	// (GitHub #839).
	version      string
	validateOpts []validation.Option
}

// newResolution returns the pass over doc, the model of self, holding self
// where the resolver looks for it when reader reads other documents.
func newResolution(ctx context.Context, doc *soa.OpenAPI, sibs Siblings,
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
		reader.hold(ctx, sibs)
	}
	return &resolution{opts: resolveOpts, reader: reader, sibs: sibs, esc: self.escapes,
		version: doc.OpenAPI, validateOpts: documentOptions(doc), away: newElsewhere(), failures: map[string][]ir.Diagnostic{},
		selfPath: self.path, nameOf: self.nameOf,
		found: reachedFindings{sites: map[references.Reference]refSite{}, known: self.found,
			tree: self.tree}}
}

// visit resolves r, the reference written at site, under opts, which read it
// against the document writing it, and notes what the resolution found and
// where it ended (see elsewhere). A reference an earlier $ref's chain resolved
// is only noted. The library counts one resolved once a chain that failed
// passed through it, so which members of a failing chain are reported follows
// the walk's order (GitHub #767). A message quotes the $ref as the document
// writes it.
func (p *resolution) visit(ctx context.Context, site refSite, r resolvable, opts references.ResolveOptions) {
	var vErrs []error
	var err error
	if !r.IsResolved() {
		vErrs, err = r.Resolve(ctx, opts)
	}
	if p.reader != nil {
		vErrs, err = p.reader.settle(ctx, r, opts, vErrs, err)
	}
	c := resolutionChain(r)
	p.away.reach(site, c, opts, p.nameOf)
	p.visited = append(p.visited, visitedRef{site: site, r: r})
	t := p.esc.trailOf(c)
	p.found.note(site, t, p.unrequired(ctx, site, r, vErrs))
	if err != nil {
		p.failures[site.doc] = append(p.failures[site.doc], failureDiag(pointerOnly(site.pointer), p.esc.writtenOf(r), t, err))
	}
}

// unrequired returns vErrs, the findings resolving r at site drew, less the
// REQUIRED-field rule's artifacts in the object r reached (requiredArtifacts),
// and reports a panic finding them at site. A schema's resolution draws no
// artifact of the rule, but is judged alike rather than told apart.
func (p *resolution) unrequired(ctx context.Context, site refSite, r resolvable, vErrs []error) []error {
	obj, _, ok := reachedObject(r)
	if !ok {
		return vErrs
	}
	required := findRequiredArtifacts(ctx, vErrs, reachedWalk(ctx, obj), p.version, p.validateOpts).reportedAt(site.pointer)
	p.failures[site.doc] = append(p.failures[site.doc], required.faults(pointerOnly)...)
	return required.without(vErrs)
}

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
// A panic in the walk or under visit becomes an error naming what was running,
// as a parser panic does in unmarshal (see recovered). It stops the walk at
// site, the model being visited, or the root when the walk panicked. An error
// from visit stops it too.
func eachSighting[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, soa.Locations, T) error,
) (site jsontext.Pointer, err error) {
	defer recovered(&err, what)
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
			return site, err
		}
	}
	return "", nil
}

// resolverPanics names the work of the reference resolver in a panic it
// recovers from.
const resolverPanics = "reference resolver"

// recovered turns a panic in the deferring function into *err, naming what was
// running. The library panics on the caller's goroutine on some shapes its
// parser accepts, such as a $ref with no value
// (docs/parser-library.md#recoverable-panic).
func recovered(err *error, what string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", what, r)
	}
}

// reachedFindings holds the findings resolveWith's resolutions draw until it
// reports them, and for each target the least pointer among the $refs whose
// trails end at it. Which $ref draws a finding depends on declaration order:
// the library hands a later $ref the object an earlier one built, and with it
// no findings. The set of $refs reaching a target does not, so a finding
// placed at a $ref is placed by that.
type reachedFindings struct {
	sites   map[references.Reference]refSite
	pending []pendingFinding
	// known holds the findings the source's own validation reported, which are
	// not reported again.
	known map[findingKey]bool
	// tree places a finding about a node of the source (see reachedFinding).
	tree sourceTree
}

// pendingFinding is a finding as the $ref at site drew it, along a trail ending
// at target, or at no target.
type pendingFinding struct {
	site   refSite
	target references.Reference
	place  string
	err    error
}

// note records that the $ref at site has trail t, and the findings vErrs its
// resolution drew, less any the source's own findings would drop (dropped).
func (f *reachedFindings) note(site refSite, t trail, vErrs []error) {
	if t.target != "" {
		if least, ok := f.sites[t.target]; !ok || compareSites(site, least) < 0 {
			f.sites[t.target] = site
		}
	}
	if len(vErrs) == 0 {
		return
	}
	place := findingPlace(t)
	for _, ve := range vErrs {
		if verr, ok := asValidationError(ve); ok && dropped(verr) {
			continue
		}
		f.pending = append(f.pending, pendingFinding{site: site, target: t.target, place: place, err: ve})
	}
}

// diags reports each finding once, where reachedFinding places it: at the node
// of the source it is about, or else at its target's least $ref, or its own
// $ref when its trail ended at no target, each $ref placed by place. None that
// is known is reported. A finding drawn more than once is kept at the least of
// its places: the library caches no object it builds from a document whose
// bytes it already holds, so it builds a target again for each $ref once
// another read that document.
func (f *reachedFindings) diags(place func(refSite) ir.Provenance) []ir.Diagnostic {
	placed := make([]refSite, len(f.pending))
	kept := make(map[findingKey]int, len(f.pending))
	for i, p := range f.pending {
		placed[i] = p.site
		if least, ok := f.sites[p.target]; ok {
			placed[i] = least
		}
		key := keyOf(p.err, placed[i])
		if j, seen := kept[key]; !seen || compareSites(placed[i], placed[j]) < 0 {
			kept[key] = i
		}
	}
	out := make([]ir.Diagnostic, 0, len(kept))
	for i, p := range f.pending {
		if key := keyOf(p.err, placed[i]); kept[key] == i && !f.known[key] {
			out = append(out, reachedFinding(f.tree, place(placed[i]), p.place, p.err))
		}
	}
	return out
}

// findingKey identifies a finding by the node it is about and what it says.
// The node is the parsed node itself, which two documents cannot share. A
// finding at no node is told apart by where it is placed as well.
type findingKey struct {
	node    *yaml.Node
	site    refSite
	rule    string
	message string
}

// keyOf returns the key of a finding placed at site.
func keyOf(err error, site refSite) findingKey {
	verr, ok := asValidationError(err)
	switch {
	case !ok:
		return findingKey{site: site, message: err.Error()}
	case verr.Node == nil:
		return findingKey{site: site, rule: verr.Rule, message: validationMessage(verr)}
	default:
		return findingKey{node: verr.Node, rule: verr.Rule, message: validationMessage(verr)}
	}
}

// failureDiag reports that the reference ref could not be resolved, quoting it
// as written, with the resolver's reason. When t got past it and stopped at
// another reference, that one is quoted too: the reason is about it, and need
// not fit ref, as "external reference not allowed" does not fit a $ref to
// #/components.
func failureDiag(site ir.Provenance, ref references.Reference, t trail, err error) ir.Diagnostic {
	reason := err.Error()
	var unparsable *bind.UnparsableError
	switch {
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

// reachedFinding converts a finding the resolver or validation made about the
// object a reference names. A structured finding keeps its rule and severity,
// as validationDiag keeps them for the source's own.
//
// One about a node of the source's tree is placed as the source's own findings
// are, at the node (GitHub #879). Any other is placed at site, the $ref that
// reached it, and its message names its position in the document place names:
// the resolution draws it before the documents Document.Sources lists are all
// read, so it is not yet placed in its own (GitHub #1030).
func reachedFinding(tree sourceTree, site ir.Provenance, place string, err error) ir.Diagnostic {
	verr, ok := asValidationError(err)
	if !ok {
		return diag.Newf(ir.SeverityError, diag.Validation, site, "%s", err.Error())
	}
	if _, held := tree.pointer(verr.Node); held {
		return validationDiag(tree.locate, err)
	}
	msg := validationMessage(verr)
	if verr.Node != nil {
		msg = fmt.Sprintf("%s, at %d:%d of %s", msg, verr.Node.Line, verr.Node.Column, place)
	}
	return diag.Newf(mapSeverity(verr.Severity), diag.Validation+"/"+verr.Rule, site, "%s", msg)
}
