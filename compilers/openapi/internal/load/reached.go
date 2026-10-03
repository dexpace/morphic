package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// reached validates what a reference brings in from another document, holding
// it to the checks the source's own objects get (GitHub #545). The source is
// validated whole before its references resolve, and the library validates a
// reference as its $ref alone, never what it names.
//
// Only what a reference reaches is validated: a shared file of components is
// used a piece at a time, and a finding in a piece nothing lowers would fail a
// compile over content it never read. Each
// finding is reported once (see check), and what validation spans is charged
// to a budget (see charge).
type reached struct {
	opts    []validation.Option
	version string
	minor   string
	path    string
	limit   int
	// covered holds every node an object already validated spans.
	covered map[*yaml.Node]bool
	// reported holds every finding already reported (see keyOf).
	reported map[findingKey]bool
	// spent is the node count charged to each document so far.
	spent map[string]int
	// over names the documents whose budget has been reported crossed.
	over map[string]bool
}

// validateReached validates, once, every object a resolved reference in doc
// brought in from another document, and reports what that finds at the $ref
// that reached it (see reached).
//
// The library's validation faults on shapes it did not expect as its resolver
// does, and this runs outside the resolver's barrier, so it has one of its own:
// a panic stops the walk and is reported at the reference being validated, or
// at the document root when the walk itself faulted.
func validateReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI,
	path string, opts Options,
) []ir.Diagnostic {
	return checkReached(ctx, at, soa.Walk(ctx, doc), newReached(doc, path, opts))
}

// checkReached is validateReached's own body, split out so a test can hand it
// a walk it controls — the same reason eachReached itself takes one (see
// matchSchemas) — since no real document can make check() panic once
// newReached has supplied it the root document a security requirement needs
// (that panic is reachable only by a document check() itself would refuse to
// build from, which is what TestResolve_ASecurityRequirementInAReachedOperationDoesNotPanic's
// own mutation exercises instead).
func checkReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance,
	items iter.Seq[soa.WalkItem], checks *reached,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	site, err := eachReached(items, func(site jsontext.Pointer, r resolvable) error {
		diags = append(diags, checks.check(ctx, at(site), r)...)
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return diags
}

// eachReached calls visit with every resolved reference the walk reaches that
// leaves the source, and the pointer that writes it, converting a panic into
// an error as eachReference does and stopping at the first error visit
// returns. validateReached's visit returns none; the stop is there so the error
// Match hands back is handled rather than discarded, as in matchSchemas.
func eachReached(items iter.Seq[soa.WalkItem], visit func(jsontext.Pointer, resolvable) error) (site jsontext.Pointer, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("validation panicked: %v", r)
		}
	}()
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() || !r.IsResolved() || r.GetReference().GetURI() == "" {
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

// newReached returns the validator for the objects doc's references reach,
// under opts' node and alias budgets; either one unbounded leaves the charge
// unbounded. It validates with the options the source's validation passes
// down: the root document, which a security requirement cannot be checked
// without, and its version.
func newReached(doc *soa.OpenAPI, path string, opts Options) *reached {
	limit := 0
	if opts.MaxSourceNodes > 0 && opts.MaxAliasSurplus > 0 {
		limit = opts.MaxSourceNodes + opts.MaxAliasSurplus
	}
	version := doc.OpenAPI
	minor, _ := SupportedMinor(version) // Load refused an unsupported version before resolving
	return &reached{
		opts: []validation.Option{
			validation.WithContextObject(doc),
			validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}),
		},
		version:  version,
		minor:    minor,
		path:     path,
		limit:    limit,
		covered:  map[*yaml.Node]bool{},
		reported: map[findingKey]bool{},
		spent:    map[string]int{},
		over:     map[string]bool{},
	}
}

// check validates the object r resolved to, when it has not been validated
// yet, and reports what it finds at site. r is a resolved reference that
// leaves the source (eachReached). Findings are reconciled as the source's are
// (dropped, and the wrong-meta-schema artifacts), and each is reported once, at
// the first $ref whose object holds it, as the source reports a finding once
// however many references share its node. An object inside one already
// validated is not validated again.
func (v *reached) check(ctx context.Context, site ir.Provenance, r resolvable) []ir.Diagnostic {
	obj, node, ok := reachedObject(r)
	if !ok || v.covered[node] {
		return nil
	}
	if d, over := v.charge(site, v.document(r), node); over {
		return d
	}

	artifacts := v.artifacts(ctx, obj)
	place := findingPlace(resolutionTrail(r))
	var diags []ir.Diagnostic
	for _, f := range validateObject(ctx, obj, v.opts) {
		if verr, ok := asValidationError(f); ok && (dropped(verr) || artifacts[findingSite(verr)]) {
			continue
		}
		if key := keyOf(f, site.Pointer); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, reachedFinding(site, place, f))
		}
	}
	return diags
}

// charge counts what validating the object at node spans against doc's budget,
// each alias counted as a copy of what it names, as validation walks it. A
// document may be charged what the source may be validated over: the node
// budget plus the alias budget. Objects nest, so without the charge a document
// could be validated many times over its size.
//
// It reports true when the object is not to be validated: doc's budget was
// crossed before, or this object would cross it, which is reported once, here.
func (v *reached) charge(site ir.Provenance, doc string, node *yaml.Node) ([]ir.Diagnostic, bool) {
	if v.over[doc] {
		return nil, true
	}
	span := spanned(node, v.remaining(doc), v.covered)
	if exceeds(v.spent[doc]+span, v.limit) {
		v.over[doc] = true
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, site,
			"the objects references reach in %s span more than the %d nodes a document is validated over; "+
				"this one, and any reached after it, is not validated", doc, v.limit)}, true
	}
	v.spent[doc] += span
	return nil, false
}

// document names the document r's first step leaves the source for, as the
// resolver spells it, so every reference into one document is charged to one
// budget. A reference the resolver could not place is charged under its own
// spelling.
func (v *reached) document(r resolvable) string {
	abs, err := references.ResolveAbsoluteReference(r.GetReference(), v.path)
	if err != nil {
		return r.GetReference().GetURI()
	}
	return abs.AbsoluteReference
}

// remaining is how many more nodes doc may be charged before its budget is
// crossed, and so how far a count need go to tell; an unbounded budget leaves
// the count unbounded too.
func (v *reached) remaining(doc string) int {
	if v.limit <= 0 {
		return -1
	}
	return v.limit - v.spent[doc]
}

// artifacts returns the sites of the schema findings obj's validation raises
// only because the library checked a schema against the wrong meta-schema, the
// reconciliation metaSchemaVersionArtifacts makes for the source's.
func (v *reached) artifacts(ctx context.Context, obj any) map[string]bool {
	if v.minor != metaSchemaReconciledMinor {
		return nil
	}
	return schemaArtifacts(ctx, func() iter.Seq[soa.WalkItem] { return reachedWalk(ctx, obj) }, v.version)
}

// reachedObject returns the object a resolved reference names and the node it
// was built from, or false for one with none to validate: a boolean schema has
// nothing but its value, and a chain that loops, or stops at a reference it
// could not resolve, ends on no object. The reference reads the node itself:
// asked of a missing object, a model's own GetRootNode faults.
func reachedObject(r resolvable) (any, *yaml.Node, bool) {
	switch ref := r.(type) {
	case *oas3.JSONSchema[oas3.Referenceable]:
		s := ref.GetResolvedSchema()
		if s.GetSchema() == nil {
			return nil, nil, false
		}
		return s, s.GetSchema().GetRootNode(), true
	default:
		holder, ok := r.(interface {
			GetObjectAny() any
			GetRootNode() *yaml.Node
		})
		if !ok {
			return nil, nil, false
		}
		node := holder.GetRootNode()
		if node == nil {
			return nil, nil, false
		}
		return holder.GetObjectAny(), node, true
	}
}

// validateObject validates obj under opts through its own Validate, as the
// source's validation reaches each object. A schema's Validate drops opts, so
// a reached schema is checked against the meta-schema a source schema is, and
// reconciled the same way (see artifacts): validated at the document's
// version, a 3.0 schema would draw findings the source's never do.
func validateObject(ctx context.Context, obj any, opts []validation.Option) []error {
	o, ok := obj.(interface {
		Validate(ctx context.Context, opts ...validation.Option) []error
	})
	if !ok {
		return nil
	}
	return o.Validate(ctx, opts...)
}

// reachedWalk returns the walk over obj, for the reconciliation of its schema
// findings. An example, a link and a security scheme hold no schema, so there
// is nothing to reconcile in one.
//
// Each kind is walked through a Reference wrapper holding it inline. The
// library's Walk dispatches on the started type, and its registry has no entry
// for a bare PathItem, Parameter, Header, RequestBody, Response or Callback, so
// a walk started from one panics (v1.25.2). A wrapper with no Reference walks
// straight through to the Object's fields, so what the walk finds is unchanged.
func reachedWalk(ctx context.Context, obj any) iter.Seq[soa.WalkItem] {
	switch o := obj.(type) {
	case *oas3.JSONSchema[oas3.Concrete]:
		return soa.Walk(ctx, oas3.ConcreteToReferenceable(o))
	case *soa.PathItem:
		return soa.Walk(ctx, &soa.ReferencedPathItem{Object: o})
	case *soa.Parameter:
		return soa.Walk(ctx, &soa.ReferencedParameter{Object: o})
	case *soa.Header:
		return soa.Walk(ctx, &soa.ReferencedHeader{Object: o})
	case *soa.RequestBody:
		return soa.Walk(ctx, &soa.ReferencedRequestBody{Object: o})
	case *soa.Response:
		return soa.Walk(ctx, &soa.ReferencedResponse{Object: o})
	case *soa.Callback:
		return soa.Walk(ctx, &soa.ReferencedCallback{Object: o})
	default:
		return func(func(soa.WalkItem) bool) {}
	}
}

// spanned counts the nodes under root as a model built from it holds them,
// each alias counted as a copy of what it names, and records each in covered.
// It stops once the count passes limit, negative meaning none. It is iterative,
// and bounded by limit or by the tree: a document whose anchor names one of its
// own ancestors was refused, so no alias leads back up.
//
// root and every node under it are real: yaml.v3 leaves no nil in Content, and
// a well-formed alias's target is never nil, or the cycle refusals ahead
// would have refused the document.
func spanned(root *yaml.Node, limit int, covered map[*yaml.Node]bool) int {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 && (limit < 0 || count <= limit) {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		covered[n] = true
		if n.Kind == yaml.AliasNode {
			stack = append(stack, n.Alias)
			continue
		}
		stack = append(stack, n.Content...)
	}
	return count
}
