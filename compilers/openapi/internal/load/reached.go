package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
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
// compile over content it never read. Each finding is reported once (see
// checkAll), and what validation spans is charged to a budget (see charge).
type reached struct {
	opts    []validation.Option
	version string
	minor   string
	limit   int
	// reported holds every finding already reported (see keyOf).
	reported map[findingKey]bool
	// spent is the node count charged to each document so far.
	spent map[string]int
	// over names the documents whose budget has been reported crossed.
	over map[string]bool
}

// reachedTarget is an object the source's references reach in another
// document: the least pointer among the $refs reaching it, the object and the
// node it was built from, and the document it is in, as findingPlace names it.
type reachedTarget struct {
	site jsontext.Pointer
	obj  any
	node *yaml.Node
	doc  string
}

// targetKey identifies a target by the node its object was built from and the
// kind a $ref read it as: a node one $ref reads as a response and another as a
// schema is validated as each.
type targetKey struct {
	node *yaml.Node
	kind reflect.Type
}

// validateReached validates every object a resolved reference in doc brings
// in from another document, and reports each finding once, at a $ref that
// reaches it (see reached).
func validateReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI,
	opts Options,
) []ir.Diagnostic {
	return checkReached(ctx, at, soa.Walk(ctx, doc), newReached(doc, opts))
}

// checkReached is validateReached over a walk the caller supplies, so a test
// can hand it one yielding a reference no real document builds.
//
// The library's validation faults on shapes it did not expect as its resolver
// does, and this runs outside the resolver's barrier, so it has one of its own.
// A panic reading the walk is reported at the reference being read, or at the
// root, and nothing is validated, since the targets read so far need not hold
// each one's least $ref.
func checkReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance,
	items iter.Seq[soa.WalkItem], checks *reached,
) []ir.Diagnostic {
	targets := map[targetKey]reachedTarget{}
	site, err := eachReached(items, func(site jsontext.Pointer, r resolvable) error {
		t, ok := targetOf(site, r)
		if !ok {
			return nil
		}
		key := targetKey{node: t.node, kind: reflect.TypeOf(t.obj)}
		if prev, seen := targets[key]; !seen || site < prev.site {
			targets[key] = t
		}
		return nil
	})
	if err != nil {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error())}
	}
	return checks.checkAll(ctx, at, targets)
}

// eachReached calls visit with every resolved reference the walk reaches, and
// the pointer that writes it, converting a panic into an error as
// eachReference does and stopping at the first error visit returns.
// checkReached's visit returns none; the stop is there so the error Match
// hands back is handled rather than discarded, as in matchSchemas.
func eachReached(items iter.Seq[soa.WalkItem], visit func(jsontext.Pointer, resolvable) error) (site jsontext.Pointer, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("validation panicked: %v", r)
		}
	}()
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() || !r.IsResolved() {
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

// targetOf returns the object the reference at site reaches in another
// document, or false when it reaches none: its chain ends on no object
// (reachedObject), or in the source, whose own validation covers what is
// there. An internal $ref whose chain leaves the source reaches an object as
// an external one does, so a finding lands where the resolver's would. A $ref
// naming the source's own file is read as another document (GitHub #759), so
// what it reaches is validated a second time.
func targetOf(site jsontext.Pointer, r resolvable) (reachedTarget, bool) {
	t := resolutionTrail(r)
	if t.endsInSource {
		return reachedTarget{}, false
	}
	obj, node, ok := reachedObject(r)
	if !ok {
		return reachedTarget{}, false
	}
	return reachedTarget{site: site, obj: obj, node: node, doc: findingPlace(t)}, true
}

// newReached returns the validator for the objects doc's references reach,
// under opts' node and alias budgets; either one unbounded leaves the charge
// unbounded. It validates with the options the source's validation passes
// down: the root document, which a security requirement cannot be checked
// without, and its version.
func newReached(doc *soa.OpenAPI, opts Options) *reached {
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
		limit:    limit,
		reported: map[findingKey]bool{},
		spent:    map[string]int{},
		over:     map[string]bool{},
	}
}

// checkAll validates each target in the order of its site, and reports what
// each finds there. A finding already reported was reported at a lesser $ref,
// so each lands at the least $ref whose object's validation draws it, and a
// budget is crossed at the same object, whatever the declaration order.
//
// An object inside one validated before it is validated again: a container
// spans more than its model reads, such as an extension's content or a node
// read as another kind.
//
// A panic in the library's validation stops it, and is reported at the target
// being validated.
func (v *reached) checkAll(ctx context.Context, at func(jsontext.Pointer) ir.Provenance,
	targets map[targetKey]reachedTarget,
) (diags []ir.Diagnostic) {
	order := slices.SortedFunc(maps.Values(targets), func(a, b reachedTarget) int {
		return strings.Compare(string(a.site), string(b.site))
	})
	var site jsontext.Pointer
	defer func() {
		if r := recover(); r != nil {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site),
				"validation panicked: %v", r))
		}
	}()
	for _, t := range order {
		site = t.site
		diags = append(diags, v.check(ctx, at(t.site), t)...)
	}
	return diags
}

// check validates t's object and reports what it finds at site. Findings are
// reconciled as the source's are (dropped, and the wrong-meta-schema
// artifacts), and one at a node already reported is not reported again, as the
// source reports a finding once however many references share its node.
func (v *reached) check(ctx context.Context, site ir.Provenance, t reachedTarget) []ir.Diagnostic {
	if d, over := v.charge(site, t.doc, t.node); over {
		return d
	}

	artifacts := v.artifacts(ctx, t.obj)
	var diags []ir.Diagnostic
	for _, f := range validateObject(ctx, t.obj, v.opts) {
		if verr, ok := asValidationError(f); ok && (dropped(verr) || artifacts[findingSite(verr)]) {
			continue
		}
		if key := keyOf(f, t.site); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, reachedFinding(site, t.doc, f))
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
	span, fits := spanned(node, v.remaining(doc))
	if !fits {
		v.over[doc] = true
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, site,
			"the objects references reach in %s span more than the %d nodes a document is validated over; "+
				"its validation stops at this one", doc, v.limit)}, true
	}
	v.spent[doc] += span
	return nil, false
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
// each alias counted as a copy of what it names, and reports whether the count
// stays within limit, negative meaning none. It stops one node past limit, so
// refusing an object costs the budget, not the object. No alias leads back up:
// a document whose anchor names one of its own ancestors was refused.
//
// root and every node under it are real: yaml.v3 leaves no nil in Content, and
// a well-formed alias's target is never nil, or the cycle refusals ahead
// would have refused the document.
func spanned(root *yaml.Node, limit int) (int, bool) {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if limit >= 0 && count > limit {
			return count, false
		}
		if n.Kind == yaml.AliasNode {
			stack = append(stack, n.Alias)
			continue
		}
		stack = append(stack, n.Content...)
	}
	return count, true
}
