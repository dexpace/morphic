package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// refKey is the key that makes a Path Item Object a reference.
const refKey = "$ref"

// Siblings holds what the source writes beside its path items' $refs, each use
// site's fields modelled as a Path Item of their own. The library's reference
// wrapper keeps none of them: its Object is nil on the $ref branch, so the
// operations, parameters, servers, x-* and undeclared keys beside a $ref reach
// no model (GitHub #577). Every pass of this phase reads them as part of the
// source, so their references resolve, and their findings are reported, as the
// document's own are. The zero value holds none.
type Siblings struct {
	byNode map[*yaml.Node]int
	list   []Sibling
}

// Sibling is the fields one use site writes beside its $ref: the Path Item they
// model, and the pointer of the first position the walk met them at.
type Sibling struct {
	Item    *soa.PathItem
	Pointer jsontext.Pointer
}

// Of returns the fields written beside rp's $ref, and false when rp is no
// reference or writes nothing beside one.
//
// It answers by the mapping the use site is written as, not by rp, so a
// reference the resolver reached through a chain finds the fields its own
// wrapper was built from, and an alias reaching one mapping twice finds one
// model.
func (s Siblings) Of(rp *soa.ReferencedPathItem) (Sibling, bool) {
	if !rp.IsReference() {
		return Sibling{}, false
	}
	i, ok := s.byNode[useSite(rp)]
	if !ok || i < 0 {
		return Sibling{}, false
	}
	return s.list[i], true
}

// Unread reports whether rp writes fields beside its $ref that no model holds:
// a reference in another document, whose content this phase does not walk.
// One the parser faulted on was reported where it is written, so it is not.
func (s Siblings) Unread(rp *soa.ReferencedPathItem) bool {
	if !rp.IsReference() {
		return false
	}
	node := useSite(rp)
	if _, met := s.byNode[node]; met || node == nil {
		return false
	}
	_, beside := withoutRef(node)
	return beside
}

// useSite returns the mapping rp, which must not be nil, is written as, through
// any alias, or nil when rp has no node.
func useSite(rp *soa.ReferencedPathItem) *yaml.Node {
	return nodeview.Deref(rp.GetCore().GetRootNode())
}

// siblingBuild models the fields of one use site, or reports why it could not.
type siblingBuild func(ctx context.Context, node *yaml.Node) (*soa.PathItem, []error, error)

// modelSiblings models the fields beside every path item $ref the walk of doc
// reaches, and beside every one inside what it models, returning the findings
// their model build and validation made, and an error diagnostic for each use
// site the parser faulted on.
func modelSiblings(ctx context.Context, doc *soa.OpenAPI, locate scan.Locator) (Siblings, []error, []ir.Diagnostic) {
	return siblingsFrom(ctx, soa.Walk(ctx, doc), locate, siblingModel(doc))
}

// siblingsFrom is modelSiblings over a walk and a model build the caller
// supplies, so a test can hand it a walk that panics, or a build that faults.
//
// The doc's walk comes first, then each model's in the order it was found, so
// a model found inside another is walked too. Each mapping is modelled once,
// so there are no more models than mappings the source holds, which its node
// bound caps. A walk that panics stops the search where it stands; the
// resolution walk meets the same fault at the same model, and reports it.
func siblingsFrom(ctx context.Context, items iter.Seq[soa.WalkItem], locate scan.Locator, build siblingBuild,
) (Siblings, []error, []ir.Diagnostic) {
	s := Siblings{byNode: map[*yaml.Node]int{}}
	var errs []error
	var faults []ir.Diagnostic
	for next := 0; next <= len(s.list) && next < sourceindex.MaxIndexedNodes; next++ {
		if next > 0 {
			items = s.walkOf(ctx, next-1)
		}
		if _, err := eachModel(items, "sibling search", func(site jsontext.Pointer, rp *soa.ReferencedPathItem) error {
			built, fault := s.add(ctx, site, rp, build, locate)
			errs = append(errs, built...)
			faults = append(faults, fault...)
			return nil
		}); err != nil {
			break
		}
	}
	return s, errs, faults
}

// add models the fields rp's use site writes beside its $ref, at site, and
// returns the findings the model build made. It does nothing for an rp that is
// no reference, nor for a mapping already met or holding nothing beside the
// $ref. A mapping the parser faulted on holds no model, and its fault is
// returned as an error diagnostic instead.
func (s *Siblings) add(ctx context.Context, site jsontext.Pointer, rp *soa.ReferencedPathItem, build siblingBuild,
	locate scan.Locator,
) ([]error, []ir.Diagnostic) {
	if !rp.IsReference() {
		return nil, nil
	}
	node := useSite(rp)
	if _, seen := s.byNode[node]; seen || node == nil {
		return nil, nil
	}
	fields, ok := withoutRef(node)
	if !ok {
		return nil, nil
	}
	item, errs, err := build(ctx, fields)
	if err != nil {
		s.byNode[node] = -1 // met, so the fault is reported once
		return nil, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, locate(node),
			"the fields written beside this path item's $ref could not be read: %s", err.Error())}
	}
	s.byNode[node] = len(s.list)
	s.list = append(s.list, Sibling{Item: item, Pointer: site})
	return errs, nil
}

// withoutRef returns a copy of the mapping node less every pair keyed $ref,
// spelled directly or through an alias as the library reads the key, and false
// when that leaves nothing. The copy keeps node's position and the document's
// own key and value nodes, so every finding and raw read lands where the source
// writes it.
func withoutRef(node *yaml.Node) (*yaml.Node, bool) {
	if node.Kind != yaml.MappingNode {
		return nil, false
	}
	fields := *node
	fields.Content = make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key := nodeview.Deref(node.Content[i]); key != nil && key.Value == refKey {
			continue
		}
		fields.Content = append(fields.Content, node.Content[i], node.Content[i+1])
	}
	if len(fields.Content) == 0 {
		return nil, false
	}
	return &fields, true
}

// siblingModel builds the fields beside a $ref into a Path Item and validates it
// as the source's own path items are, with the document as its context: the
// version selects the rules a 3.2 field is checked under.
func siblingModel(doc *soa.OpenAPI) siblingBuild {
	version := doc.OpenAPI
	opts := []validation.Option{
		validation.WithContextObject(doc),
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}),
	}
	return func(ctx context.Context, node *yaml.Node) (item *soa.PathItem, errs []error, err error) {
		defer recovered(&err, "parser")
		item = soa.NewPathItem()
		errs, err = marshaller.UnmarshalNode(ctx, "", node, item)
		return item, append(errs, item.Validate(ctx, opts...)...), err
	}
}

// walkOf returns the walk of the i-th model, located where its use site writes
// it, less the wrapper the walk starts from, which the source does not write.
// A model found inside it is not entered: siblingsFrom queues each in turn.
func (s Siblings) walkOf(ctx context.Context, i int) iter.Seq[soa.WalkItem] {
	prefix := locationOf(s.list[i].Pointer)
	start := &soa.ReferencedPathItem{Object: s.list[i].Item}
	return func(yield func(soa.WalkItem) bool) {
		for item := range soa.Walk(ctx, start) {
			if len(item.Location) == 0 {
				continue
			}
			item.Location = append(slices.Clip(prefix), item.Location...)
			if !yield(item) {
				return
			}
		}
	}
}

// walkSource returns the walk of doc with the fields beside each path item $ref
// walked right after the $ref, so every pass meets them in the order the source
// writes them, as it meets an inline item's: which members of a failing chain
// are reported follows that order (GitHub #767).
func walkSource(ctx context.Context, doc *soa.OpenAPI, s Siblings) iter.Seq[soa.WalkItem] {
	return s.interleaved(ctx, soa.Walk(ctx, doc))
}

// interleaved yields items and, after each $ref whose fields were modelled where
// the walk meets it, that model's walk, interleaved alike. The open walks are a
// stack rather than a recursion, and each model is entered once, at the one
// position it was found at, so the stack holds at most one walk per model.
func (s Siblings) interleaved(ctx context.Context, items iter.Seq[soa.WalkItem]) iter.Seq[soa.WalkItem] {
	return func(yield func(soa.WalkItem) bool) {
		var open []func() (soa.WalkItem, bool)
		var stops []func()
		defer func() {
			for _, stop := range stops {
				stop()
			}
		}()
		push := func(seq iter.Seq[soa.WalkItem]) {
			next, stop := iter.Pull(seq)
			open, stops = append(open, next), append(stops, stop)
		}
		push(items)
		for len(open) > 0 {
			item, ok := open[len(open)-1]()
			if !ok {
				open = open[:len(open)-1]
				continue
			}
			if !yield(item) {
				return
			}
			if i, entered := s.entry(item); entered {
				push(s.walkOf(ctx, i))
			}
		}
	}
}

// entry returns the model of the fields beside the $ref item is, when item is
// met at the position that model was found at.
func (s Siblings) entry(item soa.WalkItem) (int, bool) {
	if len(s.list) == 0 {
		return 0, false
	}
	at, entered := 0, false
	// Match returns only what its matcher does, and this one never fails.
	_ = item.Match(soa.Matcher{ReferencedPathItem: func(rp *soa.ReferencedPathItem) error {
		i, ok := s.byNode[useSite(rp)]
		entered = ok && i >= 0 && s.list[i].Pointer == jsontext.Pointer(item.Location.ToJSONPointer())
		at = i
		return nil
	}})
	return at, entered
}

// locationOf returns the walk location that renders as pointer. The first token
// is a field and each later one a key, because a location renders its first
// field without a separator and every key with one; a use site's first token
// is never empty, since it names a field of the OpenAPI Object.
func locationOf(pointer jsontext.Pointer) soa.Locations {
	var loc soa.Locations
	for token := range pointer.Tokens() {
		if len(loc) == 0 {
			loc = append(loc, soa.LocationContext{ParentField: token})
			continue
		}
		loc = append(loc, soa.LocationContext{ParentKey: &token})
	}
	return loc
}
