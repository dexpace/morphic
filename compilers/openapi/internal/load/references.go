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
// through reader when one is given, and reports each failure, and each finding
// in the object a reference names, at the $ref that produced it (GitHub #385,
// GitHub #537). It runs ResolveAllReferences' own walk, since that call names
// no reference for a failure or a finding.
//
// A finding is in a document with no entry in Document.Sources (GitHub #74), so
// its message names the document and its position there (see findingPlace). It
// is about a node, not a reference, so it is reported once, at the first $ref
// reaching it (see reportable).
func resolveWith(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, path string,
	opts Options, reader *external,
) []ir.Diagnostic {
	resolveOpts := references.ResolveOptions{
		TargetLocation:      path,
		RootDocument:        doc,
		DisableExternalRefs: !opts.AllowExternalRefs,
	}
	if reader != nil {
		resolveOpts.VirtualFS = *reader
		resolveOpts.HTTPClient = *reader
	}

	var diags []ir.Diagnostic
	reported := map[findingKey]bool{}
	site, err := eachReference(soa.Walk(ctx, doc), func(site jsontext.Pointer, r resolvable) error {
		vErrs, err := r.Resolve(ctx, resolveOpts)
		diags = append(diags, referenceDiags(at(site), r, reportable(vErrs, reported), err)...)
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(site), "%s", err.Error()))
	}
	return diags
}

// eachReference calls visit with each reference the walk reaches that is not
// yet resolved, and the pointer that writes it. The walk does not descend into
// what a resolved reference names, so each is visited once, where it is written.
//
// A panic from the third-party walk or resolver becomes an error, as a parser
// panic does in unmarshal: the resolver faults on shapes the parser accepts,
// such as a $ref with no value. It stops the walk, and site is the reference
// being resolved, or the root when the walk itself panicked. An error visit
// returns stops the walk too.
func eachReference(items iter.Seq[soa.WalkItem], visit func(jsontext.Pointer, resolvable) error) (site jsontext.Pointer, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reference resolver panicked: %v", r)
		}
	}()
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() || r.IsResolved() {
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

// findingKey identifies a finding by the node it is about and what it says.
// The node is the parsed node itself, which two documents cannot share.
type findingKey struct {
	node    *yaml.Node
	rule    string
	message string
}

// reportable returns the findings among vErrs to report, recording each in
// reported. One the source's own findings would drop (dropped) is dropped here
// too, and so is a repeat at a node already reported: the library caches no
// object it builds from a document whose bytes it already holds, so a target
// whose document another $ref read first is built again, findings and all, for
// each $ref reaching it. A finding at no node is never taken for a repeat, nor
// one in the source read again by a $ref from another document (GitHub #759).
func reportable(vErrs []error, reported map[findingKey]bool) []error {
	out := make([]error, 0, len(vErrs))
	for _, ve := range vErrs {
		verr, ok := asValidationError(ve)
		if ok && dropped(verr) {
			continue
		}
		if !ok || verr.Node == nil {
			out = append(out, ve)
			continue
		}
		key := findingKey{node: verr.Node, rule: verr.Rule, message: validationMessage(verr)}
		if reported[key] {
			continue
		}
		reported[key] = true
		out = append(out, ve)
	}
	return out
}

// referenceDiags converts what resolving r reported into diagnostics at site:
// each finding in what it names, and its failure. Both read the one trail the
// resolution recorded.
func referenceDiags(site ir.Provenance, r resolvable, vErrs []error, err error) []ir.Diagnostic {
	if len(vErrs) == 0 && err == nil {
		return nil
	}
	t := resolutionTrail(r)
	diags := make([]ir.Diagnostic, 0, len(vErrs)+1)
	place := findingPlace(t)
	for _, ve := range vErrs {
		diags = append(diags, reachedFinding(site, place, ve))
	}
	if err != nil {
		diags = append(diags, failureDiag(site, r, t, err))
	}
	return diags
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
