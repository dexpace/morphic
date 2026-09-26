package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

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
// through reader when one is given, and reports what each resolution found at
// the $ref that produced it: a failure, the refusal of an external reference
// included, and a finding in the object the reference names.
//
// It drives the walk ResolveAllReferences runs, with the options it builds, one
// reference at a time. The library's own call returns every failure joined
// into one error and every finding in one list, neither saying which reference
// it came from, so a failure could only be reported at the document root and a
// finding only at its node's line and column — which, for a document an
// external reference names, is a position in another file reported against
// this one (GitHub #385, GitHub #537).
//
// A finding is reported at the reference too, with its own rule and severity,
// and its message says where it is in the document the reference resolves to.
// That document has no entry in Document.Sources (GitHub #74), so the $ref is
// the one position in the IR's sources that can stand for it.
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
	site, err := eachReference(soa.Walk(ctx, doc), func(site jsontext.Pointer, r resolvable) error {
		vErrs, err := r.Resolve(ctx, resolveOpts)
		diags = append(diags, referenceDiags(at(site), r, vErrs, err)...)
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(site), "%s", err.Error()))
	}
	return diags
}

// eachReference calls visit with every reference the walk reaches that is not
// yet resolved, and the pointer that writes it, converting a panic from the
// third-party walk or resolver into an error — the resolve-side counterpart to
// unmarshal's barrier, needed because the resolver faults on shapes the parser
// accepts (a $ref with no value nil-derefs while populating the resolved node).
//
// The walk stops at the panic, as ResolveAllReferences did, because what the
// library left half-built is not something to resolve further. site is the
// reference being resolved when it panicked, or empty — the document root —
// when the walk itself did.
//
// A reference is visited where the document writes it: the walk does not
// descend into what a resolved reference names, so each is visited once. The
// walk stops at the first error visit returns, which is reported at that
// reference; resolve's visit returns none, and the stop is there so the error
// Match hands back is handled rather than discarded, as in matchSchemas.
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
