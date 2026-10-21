package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

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
// documents through reader when one is given. It reports each failure at the
// $ref that failed (GitHub #385). It runs ResolveAllReferences' own walk, since
// that call names no reference.
func resolveWith(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, sibs Siblings,
	self sourceDocument, opts Options, reader *external,
) []ir.Diagnostic {
	return newResolution(ctx, at, doc, sibs, self, opts, reader).run(ctx, doc)
}

// run is resolveWith's work over doc, the document p was made for.
func (p *resolution) run(ctx context.Context, doc *soa.OpenAPI) []ir.Diagnostic {
	p.fail(eachReference(walkSource(ctx, doc, p.sibs), resolverPanics, func(site jsontext.Pointer, r resolvable) error {
		p.visit(ctx, site, r)
		return nil
	}))
	return p.failures
}

// resolution is one resolver pass over a document: how each reference is
// resolved, and what the pass has found so far.
type resolution struct {
	at       func(jsontext.Pointer) ir.Provenance
	opts     references.ResolveOptions
	reader   *external
	failures []ir.Diagnostic
	// sibs is what the source writes beside its path items' $refs, walked
	// after doc as the rest of the source.
	sibs Siblings
	// esc holds the $refs rewritten for the resolver to read, which a message
	// quotes as the source writes them (GitHub #911).
	esc *escapes
}

// newResolution returns the pass over doc, the model of self, holding self
// where the resolver looks for it when reader reads other documents.
func newResolution(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, sibs Siblings,
	self sourceDocument, opts Options, reader *external,
) *resolution {
	resolveOpts := references.ResolveOptions{
		TargetLocation:      self.path,
		RootDocument:        doc,
		DisableExternalRefs: !opts.AllowExternalRefs,
	}
	if reader != nil {
		resolveOpts.VirtualFS = *reader
		resolveOpts.HTTPClient = *reader
		reader.hold(ctx, sibs)
	}
	return &resolution{at: at, opts: resolveOpts, reader: reader, sibs: sibs, esc: self.escapes}
}

// visit resolves r, the reference written at site, and reports a failure to. A
// reference an earlier $ref's chain resolved is not resolved again. The library
// counts one resolved once a chain that failed passed through it, so which
// members of a failing chain are reported follows the walk's order (GitHub
// #767). A message quotes the $ref as the source writes it. What the resolution
// finds wrong with the object it reaches is the table's to say (validateReached).
func (p *resolution) visit(ctx context.Context, site jsontext.Pointer, r resolvable) {
	var err error
	if !r.IsResolved() {
		_, err = r.Resolve(ctx, p.opts)
	}
	if p.reader != nil {
		err = p.reader.settle(ctx, r, p.opts, err)
	}
	if err != nil {
		p.failures = append(p.failures, failureDiag(p.at(site), p.esc.writtenOf(r), p.esc.trailOf(resolutionChain(r)), err))
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
