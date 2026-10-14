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
	// byNode is each modelled use site's index in list, by the mapping it is
	// written as, and faulted each use site the parser faulted on, whose fault
	// is reported once.
	byNode  map[*yaml.Node]int
	faulted map[*yaml.Node]bool
	list    []Sibling
	// at holds every position the walk met a modelled use site at, an alias's
	// included, with that use site's model.
	at *sites
}

// sites is a trie of positions by token, so Beside reads a pointer in one pass
// over its tokens, stopping at the first no position continues with: the work
// is bounded by the depth of the use sites, however long the pointer is.
type sites struct {
	next map[string]*sites
	// use is the position ending here and model its use site's model, when one
	// does.
	use   jsontext.Pointer
	model int
	ends  bool
}

// insert records model as the use site at site.
func (n *sites) insert(site jsontext.Pointer, model int) {
	for token := range site.Tokens() {
		child, ok := n.next[token]
		if !ok {
			if n.next == nil {
				n.next = map[string]*sites{}
			}
			child = &sites{}
			n.next[token] = child
		}
		n = child
	}
	n.use, n.model, n.ends = site, model, true
}

// Sibling is the fields one use site writes beside its $ref: the Path Item they
// model, and the pointer of the first position the walk met them at.
type Sibling struct {
	Item    *soa.PathItem
	Pointer jsontext.Pointer
	// written holds the keys the fields are written under, as the parser reads
	// the mapping, `<<` merge keys expanded.
	written map[string]bool
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
	if !ok {
		return Sibling{}, false
	}
	return s.list[i], true
}

// Unread reports whether rp writes fields beside its $ref that no model holds:
// a reference this phase's walk does not reach, in another document or at a
// position of the source no object it walks holds, such as under an x-* key.
// One the parser faulted on was reported where it is written, so it is not.
func (s Siblings) Unread(rp *soa.ReferencedPathItem) bool {
	if !rp.IsReference() {
		return false
	}
	node := useSite(rp)
	if _, met := s.byNode[node]; met || s.faulted[node] {
		return false
	}
	return writesBeside(node)
}

// Beside returns the use site, and the key written beside its $ref, that pointer
// passes through: a position the walk met a modelled path item $ref at,
// followed by one of the keys its fields are written under. The resolver reads
// such a pointer through the $ref into the item it names, so it never reaches
// the field the source writes there (GitHub #824), and the lowering declares
// that field at the very pointer. ok is false for any other pointer.
func (s Siblings) Beside(pointer jsontext.Pointer) (use jsontext.Pointer, key string, ok bool) {
	n := s.at
	for token := range pointer.Tokens() {
		if n == nil {
			break
		}
		if n.ends && s.list[n.model].written[token] {
			return n.use, token, true
		}
		n = n.next[token]
	}
	return "", "", false
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
//
// A tree in which no mapping keys $ref beside another key holds no use site and
// is not walked: that is nearly every document, and the walk is a pass over the
// whole model.
func modelSiblings(ctx context.Context, doc *soa.OpenAPI, locate scan.Locator) (Siblings, []error, []ir.Diagnostic) {
	if !anyNode(doc.GetRootNode(), refBeside) {
		return Siblings{}, nil, nil
	}
	return siblingsFrom(ctx, soa.Walk(ctx, doc), locate, siblingModel(doc))
}

// refBeside reports whether n is a mapping keying $ref beside another key, as
// every use site holding fields beside its $ref is (see withoutRef).
func refBeside(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	ref, other := false, false
	for i := 0; i+1 < len(n.Content); i += 2 {
		if isRefKey(n.Content[i]) {
			ref = true
		} else {
			other = true
		}
		if ref && other {
			return true
		}
	}
	return false
}

// unreported returns the findings of errs that valErrs, the document's own
// validation's, does not already make at the same node under the same rule.
// The library reads the summary and description beside a path item's $ref as
// its Reference Object's and validates them there, so a model holding them too
// would report one malformed value twice.
func unreported(errs, valErrs []error) []error {
	type finding struct {
		node *yaml.Node
		rule string
	}
	made := make(map[finding]bool, len(valErrs))
	for _, e := range valErrs {
		if verr, ok := asValidationError(e); ok && verr.Node != nil {
			made[finding{verr.Node, verr.Rule}] = true
		}
	}
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		if verr, ok := asValidationError(e); ok && made[finding{verr.Node, verr.Rule}] {
			continue
		}
		out = append(out, e)
	}
	return out
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
	s := Siblings{byNode: map[*yaml.Node]int{}, faulted: map[*yaml.Node]bool{}, at: &sites{}}
	var errs []error
	var faults []ir.Diagnostic
	search := func(items iter.Seq[soa.WalkItem]) bool {
		_, err := eachModel(items, "sibling search", func(site jsontext.Pointer, rp *soa.ReferencedPathItem) error {
			built, fault := s.add(ctx, site, rp, build, locate)
			errs = append(errs, built...)
			faults = append(faults, fault...)
			return nil
		})
		return err == nil
	}
	// The document's walk, then each model's in turn, until one panics.
	for walk, i := items, 0; search(walk) && i < len(s.list) && i < sourceindex.MaxIndexedNodes; i++ {
		walk = s.modelWalk(ctx, i)
	}
	return s, errs, faults
}

// add models the fields rp's use site writes beside its $ref, at site, and
// returns the findings the model build made. It does nothing for an rp that is
// no reference, nor for a mapping holding nothing beside the $ref; a mapping
// already met is only noted at site. A mapping the parser faulted on holds no
// model, and its fault is returned as an error diagnostic instead.
func (s *Siblings) add(ctx context.Context, site jsontext.Pointer, rp *soa.ReferencedPathItem, build siblingBuild,
	locate scan.Locator,
) ([]error, []ir.Diagnostic) {
	if !rp.IsReference() {
		return nil, nil
	}
	node := useSite(rp)
	if s.faulted[node] {
		return nil, nil
	}
	if i, seen := s.byNode[node]; seen {
		s.at.insert(site, i)
		return nil, nil
	}
	fields, ok := withoutRef(node)
	if !ok {
		return nil, nil
	}
	item, errs, err := build(ctx, fields)
	if err != nil {
		s.faulted[node] = true
		return nil, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, locate(node),
			"the fields written beside this path item's $ref could not be read: %s", err.Error())}
	}
	s.byNode[node] = len(s.list)
	s.at.insert(site, len(s.list))
	s.list = append(s.list, Sibling{Item: item, Pointer: site, written: writtenKeys(fields)})
	return errs, nil
}

// writtenKeys returns the keys the mapping fields, a use site less its $ref, is
// written under, read as the parser reads it.
func writtenKeys(fields *yaml.Node) map[string]bool {
	pairs := nodeview.New().MappingPairs(fields)
	written := make(map[string]bool, len(pairs))
	for _, p := range pairs {
		written[p.Key] = true
	}
	return written
}

// withoutRef returns a copy of the mapping node less every pair keyed $ref,
// spelled directly or through an alias as the library reads the key, and false
// when node is no mapping or that leaves nothing. The copy keeps node's
// position and the document's own key and value nodes, so every finding and raw
// read lands where the source writes it.
func withoutRef(node *yaml.Node) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	fields := *node
	fields.Content = make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		if isRefKey(node.Content[i]) {
			continue
		}
		fields.Content = append(fields.Content, node.Content[i], node.Content[i+1])
	}
	if len(fields.Content) == 0 {
		return nil, false
	}
	return &fields, true
}

// writesBeside reports whether the mapping node holds a pair keyed other than
// $ref, as withoutRef reads the keys. It stops at the first, so asking costs
// nothing like the copy withoutRef makes.
func writesBeside(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !isRefKey(node.Content[i]) {
			return true
		}
	}
	return false
}

// isRefKey reports whether key spells $ref, directly or through an alias, as
// the library reads a mapping's key.
func isRefKey(key *yaml.Node) bool {
	k := nodeview.Deref(key)
	return k != nil && k.Value == refKey
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

// modelWalk returns the walk of the i-th model, located where its use site
// writes it, less the wrapper the walk starts from, which the source does not
// write. A model found inside it is not entered: siblingsFrom queues each in
// turn.
func (s Siblings) modelWalk(ctx context.Context, i int) iter.Seq[soa.WalkItem] {
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
// are reported follows that order (GitHub #767). A document with none, which is
// nearly every one, is walked as the library walks it.
func walkSource(ctx context.Context, doc *soa.OpenAPI, s Siblings) iter.Seq[soa.WalkItem] {
	if len(s.list) == 0 {
		return soa.Walk(ctx, doc)
	}
	return s.interleaved(ctx, soa.Walk(ctx, doc))
}

// interleaved yields items and, after each $ref whose fields were modelled where
// the walk meets it, that model's walk, interleaved alike. The open walks are a
// stack rather than a recursion, and each model is entered once, at the one
// position it was found at, so the stack holds at most one walk per model
// beyond the walk of items itself.
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
				push(s.modelWalk(ctx, i))
			}
		}
	}
}

// entry returns the model of the fields beside the $ref item is, when item is
// met at the position that model was found at.
func (s Siblings) entry(item soa.WalkItem) (int, bool) {
	at, entered := 0, false
	// Match returns only what its matcher does, and this one never fails.
	_ = item.Match(soa.Matcher{ReferencedPathItem: func(rp *soa.ReferencedPathItem) error {
		i, ok := s.byNode[useSite(rp)]
		entered = ok && s.list[i].Pointer == jsontext.Pointer(item.Location.ToJSONPointer())
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
