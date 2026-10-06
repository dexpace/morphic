package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// resolvable is a reference the resolver follows: each Referenced* model and a
// schema's $ref. They share one options type, so one call resolves any of them.
type resolvable interface {
	IsReference() bool
	IsResolved() bool
	GetReference() references.Reference
	Resolve(ctx context.Context, opts references.ResolveOptions) ([]error, error)
}

// resolveWith resolves every reference in doc, reading external documents
// through reader when one is given, then every discriminator mapping target as
// a $ref to it would be (see mappings). It reports each failure at the $ref
// that failed, and each finding in what a reference names at a $ref reaching
// it (GitHub #385, GitHub #537). It runs ResolveAllReferences' own walk, since
// that call names no reference for either.
//
// A "#/$defs/..." reference is held out of that walk and resolved after it (see
// withDefsHeld). A finding is reported once (see reachedFindings), and not
// where the source's own validation reported it.
func resolveWith(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, self sourceDocument,
	opts Options, reader *external,
) (MappingTargets, []ir.Diagnostic) {
	return newResolution(ctx, at, doc, self, opts, reader).run(doc)
}

// run is resolveWith's work over doc, the document p was made for.
func (p *resolution) run(doc *soa.OpenAPI) (MappingTargets, []ir.Diagnostic) {
	held := heldRefs(p.ctx, doc, defs.NewReader(doc))
	withDefsHeld(held, func() {
		p.fail(eachSighting(soa.Walk(p.ctx, doc), resolverPanics,
			func(site jsontext.Pointer, loc soa.Locations, model any) error {
				p.targets.see(site, model)
				if r, ok := model.(resolvable); ok && r.IsReference() {
					p.visit(site, r, r.GetReference())
				}
				return nil
			}))
	})
	p.fail(p.resolveHeld(held))
	p.fail(p.resolveTargets(held))
	p.failures = append(p.failures, p.targets.exhausted(p.at)...)
	p.failures = append(p.failures, p.loops.incomplete(p.at)...)
	if p.reader != nil {
		p.failures = append(p.failures, p.reader.work.exhausted(p.at)...)
	}
	return p.targets.targets(), append(p.failures, p.found.diags(p.at)...)
}

// resolveTargets resolves the mapping targets with every held reference as
// resolveHeld resolved it, and puts each back as written after. A target's
// chain meets a "#/$defs/..." reference as a $ref's did: at its definition, or,
// where the rule names none, ending there. Put back as written, it sent the
// chain to the resolver's own lookup, which resolveHeld keeps out of play.
func (p *resolution) resolveTargets(held []heldRef) (jsontext.Pointer, error) {
	for _, r := range held {
		r.retarget()
	}
	defer restoreDefs(held)
	return p.targets.resolve(p.ctx, &p.found)
}

// resolution is one resolver pass over a document: how each reference is
// resolved, what the pass has found so far, and the mapping targets it
// collects.
type resolution struct {
	ctx     context.Context
	at      func(jsontext.Pointer) ir.Provenance
	opts    references.ResolveOptions
	reader  *external
	targets *mappings
	// loops finds the schema references whose chain never ends, which the
	// resolver is not asked to follow. It is nil unless external references are
	// read, since no other chain reaches the source by its file name.
	loops    *loops
	failures []ir.Diagnostic
	found    reachedFindings
}

// newResolution returns the pass over doc, the model of self, holding self
// where the resolver looks for it when reader reads other documents.
func newResolution(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, self sourceDocument,
	opts Options, reader *external,
) *resolution {
	resolveOpts := references.ResolveOptions{
		TargetLocation:      self.path,
		RootDocument:        doc,
		DisableExternalRefs: !opts.AllowExternalRefs,
	}
	if reader != nil {
		resolveOpts.VirtualFS = *reader
		resolveOpts.HTTPClient = *reader
		reader.hold(ctx)
	}
	p := &resolution{ctx: ctx, at: at, opts: resolveOpts, reader: reader,
		targets: newMappings(self, doc, resolveOpts, reader),
		found:   reachedFindings{sites: map[references.Reference]jsontext.Pointer{}, known: self.found}}
	if reader != nil {
		p.loops = newLoops(self, doc)
	}
	return p
}

// visit resolves r, the reference written as ref at site, and notes what the
// resolution found. A reference an earlier $ref's chain resolved is only noted.
// The library counts one resolved once a chain that failed passed through it,
// so which members of a failing chain are reported follows the walk's order
// (GitHub #767). A schema's chain is read for a loop from the $ref r carries
// now, which for a held "#/$defs/..." reference is its definition's pointer
// (see retarget); ref is only quoted.
func (p *resolution) visit(site jsontext.Pointer, r resolvable, ref references.Reference) {
	if js, schema := r.(*schemaRef); schema && p.loops != nil && !js.IsResolved() {
		looped, cut := p.loops.read(js.GetRef())
		if looped {
			p.failures = append(p.failures, diag.Newf(ir.SeverityError, diag.CyclicRef, p.at(site),
				"cyclic $ref: reference chain never reaches a node without a $ref"))
			return
		}
		if cut && p.loops.held() {
			// Unread, the chain could close through the held source's file and
			// run the stack out, so it is left to the lowering, which reports it
			// unresolved, and incomplete says why.
			return
		}
	}
	var vErrs []error
	var err error
	if !r.IsResolved() {
		vErrs, err = r.Resolve(p.ctx, p.opts)
	}
	if p.reader != nil {
		vErrs, err = p.reader.settle(p.ctx, r, p.opts, vErrs, err)
	}
	c := resolutionChain(r)
	p.targets.enqueue(site, c)
	t := c.trail()
	p.found.note(site, t, vErrs)
	if err != nil {
		p.failures = append(p.failures, failureDiag(p.at(site), ref, t, err))
	}
}

// fail reports the fault a walk ended on, at the reference it ended at, and
// nothing for no fault.
func (p *resolution) fail(site jsontext.Pointer, err error) {
	if err != nil {
		p.failures = append(p.failures, diag.Newf(ir.SeverityError, diag.UnresolvedRef, p.at(site), "%s", err.Error()))
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
// as a parser panic does in unmarshal: the library faults on shapes the parser
// accepts, such as a $ref with no value. It stops the walk at site, the model
// being visited, or the root when the walk panicked. An error from visit stops
// it too.
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
// running.
func recovered(err *error, what string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", what, r)
	}
}

// reachedFindings holds the findings resolveWith's resolutions draw, the walk's,
// the held "#/$defs/..." references' and the mapping targets', until it reports
// them, and for each target the least pointer among the $refs whose trails end
// at it. Which $ref draws a finding depends on declaration order: the library
// hands a later $ref the object an earlier one built, and with it no findings.
// The set of $refs reaching a target does not, so a finding is placed by that.
type reachedFindings struct {
	sites   map[references.Reference]jsontext.Pointer
	pending []pendingFinding
	// known holds the findings the source's own validation reported, which are
	// not reported again.
	known map[findingKey]bool
}

// pendingFinding is a finding as the $ref at site drew it, along a trail ending
// at target, or at no target. An entry's finding about a position holding no
// schema is optional (see noteEntry).
type pendingFinding struct {
	site     jsontext.Pointer
	target   references.Reference
	place    string
	err      error
	optional bool
}

// note records that the $ref at site has trail t, and the findings vErrs its
// resolution drew, less any the source's own findings would drop (dropped).
func (f *reachedFindings) note(site jsontext.Pointer, t trail, vErrs []error) {
	f.noteEntry(site, t, vErrs, nil)
}

// noteEntry is note for a mapping entry's resolution, or a $ref's in an object
// built from raw YAML, which marks optional each finding about a node in
// noSchema. One is reported only where a $ref of the model's own drew it as
// well: the library draws a finding once, for whichever resolution built its
// node first, so dropping one drawn first would make the report follow order.
func (f *reachedFindings) noteEntry(site jsontext.Pointer, t trail, vErrs []error, noSchema map[*yaml.Node]bool) {
	if t.target != "" {
		if least, ok := f.sites[t.target]; !ok || site < least {
			f.sites[t.target] = site
		}
	}
	if len(vErrs) == 0 {
		return
	}
	place := findingPlace(t)
	for _, ve := range vErrs {
		verr, ok := asValidationError(ve)
		if ok && dropped(verr) {
			continue
		}
		f.pending = append(f.pending, pendingFinding{site: site, target: t.target, place: place, err: ve,
			optional: ok && noSchema[verr.Node]})
	}
}

// diags reports each finding once, at its target's least $ref, or at its own
// $ref when its trail ended at no target, and none that is known, nor one only
// ever drawn as optional (see noteEntry). A finding drawn more than once is
// kept at the least of its places: the library caches no object it builds from
// a document whose bytes it already holds, so it builds a target again for each
// $ref once another read that document.
func (f *reachedFindings) diags(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	placed := make([]jsontext.Pointer, len(f.pending))
	kept := make(map[findingKey]int, len(f.pending))
	required := make(map[findingKey]bool, len(f.pending))
	for i, p := range f.pending {
		placed[i] = p.site
		if least, ok := f.sites[p.target]; ok {
			placed[i] = least
		}
		key := keyOf(p.err, placed[i])
		if j, seen := kept[key]; !seen || placed[i] < placed[j] {
			kept[key] = i
		}
		required[key] = required[key] || !p.optional
	}
	out := make([]ir.Diagnostic, 0, len(kept))
	for i, p := range f.pending {
		if key := keyOf(p.err, placed[i]); kept[key] == i && required[key] && !f.known[key] {
			out = append(out, reachedFinding(at(placed[i]), p.place, p.err))
		}
	}
	return out
}

// findingKey identifies a finding by the node it is about and what it says.
// The node is the parsed node itself, which two documents cannot share. A
// finding at no node is told apart by where it is placed as well.
type findingKey struct {
	node    *yaml.Node
	site    jsontext.Pointer
	rule    string
	message string
}

// keyOf returns the key of a finding placed at site.
func keyOf(err error, site jsontext.Pointer) findingKey {
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
	if len(t.docs) > 0 && t.stopped != "" {
		return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q, through %s: %s",
			string(ref), quotedStop(t), err.Error())
	}
	return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q: %s",
		string(ref), err.Error())
}

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

// reachedFinding converts a finding the resolver made while building the
// object a reference names. A structured finding keeps its rule and severity,
// as validationDiag keeps them for the source's own. Its position is in the
// document place names, which no Provenance can (GitHub #74), so the message
// carries both.
func reachedFinding(site ir.Provenance, place string, err error) ir.Diagnostic {
	verr, ok := asValidationError(err)
	if !ok {
		return diag.Newf(ir.SeverityError, diag.Validation, site, "%s", err.Error())
	}
	msg := validationMessage(verr)
	if verr.Node != nil {
		msg = fmt.Sprintf("%s, at %d:%d of %s", msg, verr.Node.Line, verr.Node.Column, place)
	}
	return diag.Newf(mapSeverity(verr.Severity), diag.Validation+"/"+verr.Rule, site, "%s", msg)
}
