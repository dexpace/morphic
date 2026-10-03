package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"

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
// GitHub #537). It runs ResolveAllReferences' own walk, since that call says
// neither which reference failed nor which one a finding came from.
//
// A finding's position is in the target document, which has no entry in
// Document.Sources (GitHub #74), so it goes in the message. A finding is about a
// node, not a reference, so it is reported once, at the first $ref reaching it
// (see unreported).
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
		diags = append(diags, referenceDiags(at(site), r, unreported(vErrs, reported), err)...)
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

// unreported returns the findings among vErrs not yet in reported, and records
// them. Only a finding placed at a node can be told apart from another like it;
// anything else is kept as it is.
func unreported(vErrs []error, reported map[findingKey]bool) []error {
	out := make([]error, 0, len(vErrs))
	for _, ve := range vErrs {
		verr, ok := asValidationError(ve)
		if !ok || verr.Node == nil {
			out = append(out, ve)
			continue
		}
		key := findingKey{node: verr.Node, rule: verr.Rule, message: ve.Error()}
		if reported[key] {
			continue
		}
		reported[key] = true
		out = append(out, ve)
	}
	return out
}

// referenceDiags converts what resolving r reported into diagnostics at site:
// its failure, naming the reference as written, and each finding in what it
// names.
func referenceDiags(site ir.Provenance, r resolvable, vErrs []error, err error) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(vErrs)+1)
	for _, ve := range vErrs {
		diags = append(diags, reachedFinding(site, ve))
	}
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, site,
			"unresolved $ref %q: %s", string(r.GetReference()), err.Error()))
	}
	return diags
}

// reachedFinding converts a finding the resolver made while building the
// object a reference names. A structured finding keeps its rule and severity,
// as validationDiag keeps them for the source's own; its position, which is in
// the document the reference resolves to, goes in the message.
func reachedFinding(site ir.Provenance, err error) ir.Diagnostic {
	verr, ok := asValidationError(err)
	if !ok {
		return diag.Newf(ir.SeverityError, diag.Validation, site, "%s", err.Error())
	}
	msg := validationMessage(verr)
	if verr.Node != nil {
		msg = fmt.Sprintf("%s, at %d:%d of the document the $ref resolves to",
			msg, verr.Node.Line, verr.Node.Column)
	}
	return diag.Newf(mapSeverity(verr.Severity), diag.Validation+"/"+verr.Rule, site, "%s", msg)
}
