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
// through reader when one is given. It reports each failure at the $ref that
// failed, and each finding in the object a reference names at a $ref reaching
// it (GitHub #385, GitHub #537). It runs ResolveAllReferences' own walk, since
// that call names no reference for a failure or a finding.
//
// A "#/$defs/..." reference is held out of that walk and resolved after it (see
// withDefsHeld). A finding is in a document with no entry in Document.Sources
// (GitHub #74), so its message names the document (see findingPlace), and is
// reported once (see reachedFindings).
func resolveWith(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, path string,
	opts Options, reader *external,
) []ir.Diagnostic {
	pass := newResolution(ctx, at, doc, path, opts, reader)
	held := defsRefs(ctx, doc)
	withDefsHeld(held, func() {
		pass.fail(eachReference(soa.Walk(ctx, doc), func(site jsontext.Pointer, r resolvable) error {
			pass.visit(site, r, r.GetReference())
			return nil
		}))
	})
	pass.fail(pass.resolveHeld(held))
	return append(pass.failures, pass.found.diags(at)...)
}

// resolution is one resolver pass over a document: how each reference is
// resolved, and what the pass has found so far.
type resolution struct {
	ctx      context.Context
	at       func(jsontext.Pointer) ir.Provenance
	opts     references.ResolveOptions
	failures []ir.Diagnostic
	found    reachedFindings
}

func newResolution(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, path string,
	opts Options, reader *external,
) *resolution {
	resolveOpts := references.ResolveOptions{
		TargetLocation:      path,
		RootDocument:        doc,
		DisableExternalRefs: !opts.AllowExternalRefs,
	}
	if reader != nil {
		resolveOpts.VirtualFS = *reader
		resolveOpts.HTTPClient = *reader
	}
	return &resolution{ctx: ctx, at: at, opts: resolveOpts,
		found: reachedFindings{sites: map[references.Reference]jsontext.Pointer{}}}
}

// visit resolves r, the reference written as ref at site, and notes what the
// resolution found. A reference an earlier $ref's chain resolved is only noted.
func (p *resolution) visit(site jsontext.Pointer, r resolvable, ref references.Reference) {
	var vErrs []error
	var err error
	if !r.IsResolved() {
		vErrs, err = r.Resolve(p.ctx, p.opts)
	}
	t := resolutionTrail(r)
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
//
// A panic from the third-party walk or resolver becomes an error, as a parser
// panic does in unmarshal: the resolver faults on shapes the parser accepts,
// such as a $ref with no value. It stops the walk, and site is the reference
// being resolved, or the root when the walk itself panicked. An error visit
// returns stops the walk too.
func eachReference(items iter.Seq[soa.WalkItem], visit func(jsontext.Pointer, resolvable) error) (site jsontext.Pointer, err error) {
	defer recovered(&err)
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() {
				return nil
			}
			site = jsontext.Pointer(item.Location.ToJSONPointer())
			if err := visit(site, r); err != nil {
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

// recovered turns a panic in the deferring function into *err.
func recovered(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("reference resolver panicked: %v", r)
	}
}

// reachedFindings holds the findings resolveWith's walk draws until the walk ends, and
// for each target the least pointer among the $refs whose trails end at it.
// Which $ref draws a finding depends on declaration order: the library hands a
// later $ref the object an earlier one built, and with it no findings. The set
// of $refs reaching a target does not, so a finding is placed by that.
type reachedFindings struct {
	sites   map[references.Reference]jsontext.Pointer
	pending []pendingFinding
}

// pendingFinding is a finding as the $ref at site drew it, along a trail ending
// at target, or at no target.
type pendingFinding struct {
	site   jsontext.Pointer
	target references.Reference
	place  string
	err    error
}

// note records that the $ref at site has trail t, and the findings vErrs its
// resolution drew, less any the source's own findings would drop (dropped).
func (f *reachedFindings) note(site jsontext.Pointer, t trail, vErrs []error) {
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
		if verr, ok := asValidationError(ve); ok && dropped(verr) {
			continue
		}
		f.pending = append(f.pending, pendingFinding{site: site, target: t.target, place: place, err: ve})
	}
}

// diags reports each finding once, at its target's least $ref, or at its own
// $ref when its trail ended at no target. A finding drawn more than once is
// kept at the least of its places: the library caches no object it builds from
// a document whose bytes it already holds, so it builds a target again for each
// $ref once another read that document. A finding in the source read again by
// a $ref from another document shares no node with the source's (GitHub #759).
func (f *reachedFindings) diags(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	placed := make([]jsontext.Pointer, len(f.pending))
	kept := make(map[findingKey]int, len(f.pending))
	for i, p := range f.pending {
		placed[i] = p.site
		if least, ok := f.sites[p.target]; ok {
			placed[i] = least
		}
		key := keyOf(p.err, placed[i])
		if j, seen := kept[key]; !seen || placed[i] < placed[j] {
			kept[key] = i
		}
	}
	out := make([]ir.Diagnostic, 0, len(kept))
	for i, p := range f.pending {
		if kept[keyOf(p.err, placed[i])] == i {
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
