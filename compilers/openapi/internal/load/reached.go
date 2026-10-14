package load

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"iter"
	"maps"
	"reflect"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// reached validates what a reference reaches that the source's own validation
// never checked, as the source's objects are checked: an object in another
// document (GitHub #545), or one in the source outside its model, such as under
// an x-* key, or read as another kind (GitHub #759, #872). The library
// validates a reference as its $ref alone.
//
// Only what a reference reaches is validated: a finding in a piece of a shared
// file nothing lowers would fail a compile over content it never read. Each
// finding is reported once (see checkAll), and what validation spans is charged
// to a budget (see charge).
type reached struct {
	doc     *soa.OpenAPI
	opts    []validation.Option
	version string
	minor   string
	limit   int
	// tree places a finding about a node of the source, and the later
	// version's constructs an object reached there writes (see later).
	tree sourceTree
	// reported holds every finding already reported (see keyOf), the source's
	// own validation's included.
	reported map[findingKey]bool
	// warned holds every later version's construct already reported: an object
	// inside another reached one is walked again (see checkAll).
	warned map[ir.Diagnostic]bool
	// spent is the node count charged to each document so far.
	spent map[string]int
	// over names the documents whose budget has been reported crossed.
	over map[string]bool
}

// reachedTarget is an object the source's references reach: the least pointer
// among the $refs reaching it, the object and the node it was built from, the
// document it is in, as findingPlace names it, and whether that is the source.
type reachedTarget struct {
	site     jsontext.Pointer
	obj      any
	node     *yaml.Node
	doc      string
	inSource bool
}

// targetKey identifies a target by the node its object was built from and the
// kind a $ref read it as: a node one $ref reads as a response and another as a
// schema is validated as each.
type targetKey struct {
	node *yaml.Node
	kind reflect.Type
}

// validateReached validates every object a resolved reference in doc reaches
// that the source's own validation did not (see targetOf and dropDeclared), and
// reports each finding once, where reachedFinding places it. A finding known,
// the source's own validation's, is not reported again: a node a $ref reads as
// another kind holds what the source declares inside it.
func validateReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI,
	targets MappingTargets, opts Options, self sourceDocument,
) []ir.Diagnostic {
	checks := newReached(doc, opts, self.tree)
	maps.Copy(checks.reported, self.found)
	return checkReached(ctx, at, soa.Walk(ctx, doc), targets.resolved, checks)
}

// checkReached is validateReached over a walk the caller supplies, so a test
// can hand it one yielding a reference no real document builds. resolved are
// the references the walk does not show (see MappingTargets.resolved).
//
// The library's validation faults on shapes it did not expect as its resolver
// does, so reading the walk and validating each run behind a barrier. A panic
// reading the walk is reported at the model being read, or at the root, and
// nothing is validated, since the targets read so far need not hold each one's
// least $ref.
func checkReached(ctx context.Context, at func(jsontext.Pointer) ir.Provenance,
	items iter.Seq[soa.WalkItem], resolved []siteRef, checks *reached,
) []ir.Diagnostic {
	targets := map[targetKey]reachedTarget{}
	add := func(site jsontext.Pointer, r resolvable) {
		t, ok := targetOf(site, r)
		if !ok {
			return
		}
		key := targetKey{node: t.node, kind: reflect.TypeOf(t.obj)}
		if prev, seen := targets[key]; !seen || site < prev.site {
			targets[key] = t
		}
	}
	site, err := eachReference(items, "validation", func(site jsontext.Pointer, r resolvable) error {
		add(site, r)
		return nil
	})
	if err == nil {
		site, err = eachResolved(resolved, add)
	}
	if err == nil {
		site, err = dropDeclared(items, targets)
	}
	if err != nil {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error())}
	}
	return checks.checkAll(ctx, at, targets)
}

// eachResolved calls visit with each of resolved, behind the barrier
// eachReference puts around a walk: a panic stops it, reported at the site
// being visited.
func eachResolved(resolved []siteRef, visit func(jsontext.Pointer, resolvable)) (site jsontext.Pointer, err error) {
	defer recovered(&err, "validation")
	for _, r := range resolved {
		site = r.site
		visit(r.site, r.ref)
	}
	return "", nil
}

// targetOf returns the object the reference at site reaches, or false when its
// chain ends on no object (reachedObject). A chain that ends in the source is a
// target too, whatever its $refs spell: it can reach raw YAML, such as an x-*
// key's value, or a node read as another kind, neither of which the source's
// validation checked. What that validation did check, dropDeclared drops.
func targetOf(site jsontext.Pointer, r resolvable) (reachedTarget, bool) {
	t := resolutionChain(r).trail()
	obj, node, ok := reachedObject(r)
	if !ok {
		return reachedTarget{}, false
	}
	return reachedTarget{site: site, obj: obj, node: node, doc: findingPlace(t), inSource: t.endsInSource}, true
}

// dropDeclared drops each target in the source that the source's own
// validation covered: an object its model declares, reached as the kind it is
// declared as, which the source reported on already (GitHub #759, #872). It
// walks items, the source's models, only when some target is in the source.
func dropDeclared(items iter.Seq[soa.WalkItem], targets map[targetKey]reachedTarget) (jsontext.Pointer, error) {
	inSource := false
	for _, t := range targets {
		inSource = inSource || t.inSource
	}
	if !inSource {
		return "", nil
	}
	return eachModel(items, "validation", func(_ jsontext.Pointer, r resolvable) error {
		if r.IsReference() {
			return nil
		}
		if obj, node, ok := reachedObject(r); ok {
			key := targetKey{node: node, kind: reflect.TypeOf(obj)}
			if targets[key].inSource {
				delete(targets, key)
			}
		}
		return nil
	})
}

// newReached returns the validator for the objects doc's references reach,
// under opts' node and alias budgets; either one unbounded leaves the charge
// unbounded. It validates with the options the source's validation passes
// down: the root document, which a security requirement cannot be checked
// without, and its version. tree is the source's (see reachedFinding).
func newReached(doc *soa.OpenAPI, opts Options, tree sourceTree) *reached {
	limit := 0
	if opts.MaxSourceNodes > 0 && opts.MaxAliasSurplus > 0 {
		limit = opts.MaxSourceNodes + opts.MaxAliasSurplus
	}
	version := doc.OpenAPI
	minor, _ := SupportedMinor(version) // Load refused an unsupported version before resolving
	return &reached{
		doc:      doc,
		tree:     tree,
		opts:     documentOptions(doc),
		version:  version,
		minor:    minor,
		limit:    limit,
		reported: map[findingKey]bool{},
		warned:   map[ir.Diagnostic]bool{},
		spent:    map[string]int{},
		over:     map[string]bool{},
	}
}

// checkAll validates each target in the order of its site, and reports its
// findings. A finding already reported was reported for a lesser $ref, so one
// placed at a $ref lands at the least whose object's validation draws it, and
// a budget is crossed at the same object, whatever the declaration order.
//
// An object inside one validated before it is validated, and charged, again
// (GitHub #571): a container spans more than its model reads, such as an
// extension's content.
//
// A panic in validation stops it, and is reported at the target it reached.
func (v *reached) checkAll(ctx context.Context, at func(jsontext.Pointer) ir.Provenance,
	targets map[targetKey]reachedTarget,
) (diags []ir.Diagnostic) {
	order := slices.SortedFunc(maps.Values(targets), func(a, b reachedTarget) int {
		return cmp.Compare(a.site, b.site)
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
		diags = append(diags, v.check(ctx, at, t)...)
	}
	return diags
}

// check validates t's object and reports what it finds at t's site. Findings
// are reconciled as the source's are (dropped, the wrong-meta-schema
// artifacts, and a later version's constructs, see later), and one at a node
// already reported is not reported again, as the source reports a finding once
// however many references share its node.
func (v *reached) check(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, t reachedTarget) []ir.Diagnostic {
	site := at(t.site)
	if d, over := v.charge(site, t.doc, t.node); over {
		return d
	}

	later := v.later(ctx, at, t)
	found := validateObject(ctx, t.obj, v.opts)
	if later.additionalOperations {
		found = ownAdditionalOperations(ctx, v.doc, reachedWalk(ctx, t.obj), found)
	}
	artifacts := v.artifacts(ctx, t.obj, later.inSchema)
	var diags []ir.Diagnostic
	for _, d := range later.diags {
		if !v.warned[d] {
			v.warned[d] = true
			diags = append(diags, d)
		}
	}
	for _, f := range found {
		if verr, ok := asValidationError(f); ok && (dropped(verr) || artifacts[siteOf(verr)]) {
			continue
		}
		if key := keyOf(f, t.site); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, reachedFinding(v.tree, site, t.doc, f))
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
// reconciliation metaSchemaVersionArtifacts makes for the source's. later says
// whether later reported a keyword inside a schema of obj.
func (v *reached) artifacts(ctx context.Context, obj any, later bool) map[findingSite]bool {
	return versionArtifacts(ctx, func() iter.Seq[soa.WalkItem] { return reachedWalk(ctx, obj) },
		v.version, v.minor, later)
}

// later reports the constructs a minor later than the document's added that
// t's object writes, at the pointers they are written at, when the source's
// tree holds the object: the source's walk never reaches it, so laterConstructs
// would not otherwise judge it, and its refusals would stand where the same
// object written inline draws a warning (GitHub #872). Another document's
// content, which the tree does not hold, is GitHub #839's.
func (v *reached) later(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, t reachedTarget) laterReport {
	base, ok := v.tree.pointer(t.node)
	if !ok {
		return laterReport{}
	}
	return laterConstructs(func(p jsontext.Pointer) ir.Provenance { return at(base + p) },
		reachedWalk(ctx, t.obj), v.version)
}

// reachedObject returns the object a reference names and the node it was built
// from, or false for one with none to validate: an unresolved reference names
// none, a boolean schema has nothing but its value, and a chain that loops, or
// stops at a reference it could not resolve, ends on no object. The reference
// reads the node itself: asked of a missing object, a model's own GetRootNode
// faults.
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

// reachedWalk returns the walk over obj, for the reconciliation of its findings
// and the later version's constructs it writes.
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
	case *soa.Example:
		return soa.Walk(ctx, &soa.ReferencedExample{Object: o})
	case *soa.Link:
		return soa.Walk(ctx, &soa.ReferencedLink{Object: o})
	case *soa.SecurityScheme:
		return soa.Walk(ctx, &soa.ReferencedSecurityScheme{Object: o})
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
