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
// through reader when one is given, then every discriminator mapping target as
// a $ref to it would be (see mappings). It reports each failure at the $ref
// that failed, and each finding in what a reference names at a $ref reaching
// it (GitHub #385, GitHub #537). It runs ResolveAllReferences' own walk, since
// that call names no reference for either.
//
// A finding's document has no entry in Document.Sources (GitHub #74), so its
// message names the document and its position there (see findingPlace). It is
// about a node, so it is reported once (see reachedFindings), and not where the
// source's own validation reported it.
func resolveWith(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, self sourceDocument,
	opts Options, reader *external,
) (MappingTargets, []ir.Diagnostic) {
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

	var failures []ir.Diagnostic
	found := reachedFindings{sites: map[references.Reference]jsontext.Pointer{}, known: self.found}
	targets := newMappings(self, doc, resolveOpts, reader)
	site, err := eachModel(soa.Walk(ctx, doc), "reference resolver", func(site jsontext.Pointer, model any) error {
		targets.see(site, model)
		r, ok := model.(resolvable)
		if !ok || !r.IsReference() {
			return nil
		}
		var vErrs []error
		var err error
		if !r.IsResolved() { // one an earlier $ref's chain resolved is only noted
			vErrs, err = r.Resolve(ctx, resolveOpts)
		}
		if reader != nil {
			vErrs, err = reader.settle(ctx, r, resolveOpts, vErrs, err)
		}
		c := resolutionChain(r)
		targets.reached(site, c)
		found.note(site, c.trail(), vErrs)
		if err != nil {
			failures = append(failures, failureDiag(at(site), r, c.trail(), err))
		}
		return nil
	})
	if err == nil {
		site, err = targets.resolve(ctx, &found)
	}
	if err != nil {
		failures = append(failures, diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(site), "%s", err.Error()))
	}
	return targets.targets(), append(failures, found.diags(at)...)
}

// eachReference calls visit with each reference the walk reaches, resolved or
// not, and the pointer that writes it. The walk does not descend into what a
// resolved reference names, so each is visited once, where it is written.
func eachReference(items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, resolvable) error,
) (jsontext.Pointer, error) {
	return eachResolvable(items, what, func(site jsontext.Pointer, r resolvable) error {
		if !r.IsReference() {
			return nil
		}
		return visit(site, r)
	})
}

// eachResolvable calls visit with each model the walk reaches that a reference
// can name or be, and the pointer that names it.
func eachResolvable(items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, resolvable) error,
) (jsontext.Pointer, error) {
	return eachModel(items, what, visit)
}

// eachModel calls visit with each model of kind T the walk reaches, and the
// pointer that names it.
//
// A panic in the walk or under visit becomes an error naming what was running,
// as a parser panic does in unmarshal: the library faults on shapes the parser
// accepts, such as a $ref with no value. It stops the walk at site, the model
// being visited, or the root when the walk panicked. An error from visit stops
// it too.
func eachModel[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, T) error,
) (site jsontext.Pointer, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s panicked: %v", what, r)
		}
	}()
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			m, ok := model.(T)
			if !ok {
				return nil
			}
			site = jsontext.Pointer(item.Location.ToJSONPointer())
			if err := visit(site, m); err != nil {
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

// reachedFindings holds the findings resolveWith's walk draws until the walk ends, and
// for each target the least pointer among the $refs whose trails end at it.
// Which $ref draws a finding depends on declaration order: the library hands a
// later $ref the object an earlier one built, and with it no findings. The set
// of $refs reaching a target does not, so a finding is placed by that.
type reachedFindings struct {
	sites   map[references.Reference]jsontext.Pointer
	pending []pendingFinding
	// known holds the findings already reported, which are not reported again.
	known map[findingKey]bool
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
// $ref when its trail ended at no target, and none that is known. A finding
// drawn more than once is kept at the least of its places: the library caches
// no object it builds from a document whose bytes it already holds, so it
// builds a target again for each $ref once another read that document.
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
		if key := keyOf(p.err, placed[i]); kept[key] == i && !f.known[key] {
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

// failureDiag reports that r could not be resolved, quoting it as written, with
// the resolver's reason. When t got past r and stopped at another reference,
// that one is quoted too: the reason is about it, and need not fit r, as
// "external reference not allowed" does not fit a $ref to #/components.
func failureDiag(site ir.Provenance, r resolvable, t trail, err error) ir.Diagnostic {
	if len(t.docs) > 0 && t.stopped != "" {
		return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q, through %s: %s",
			string(r.GetReference()), quotedStop(t), err.Error())
	}
	return diag.Newf(ir.SeverityError, diag.UnresolvedRef, site, "unresolved $ref %q: %s",
		string(r.GetReference()), err.Error())
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
