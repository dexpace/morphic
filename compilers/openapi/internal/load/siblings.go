package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
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
	// writtenAs holds the mapping each reference object a model's walk met is
	// written as, by its position there: every one but a schema, whose $ref is
	// refused by pointer alone.
	writtenAs map[jsontext.Pointer]*yaml.Node
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
	// keys holds the keys the fields are written under, as the parser reads the
	// mapping, `<<` merge keys expanded.
	keys map[string]bool
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
	n, key, ok := s.useOf(pointer)
	if !ok {
		return "", "", false
	}
	return n.use, key, true
}

// useOf returns the position Beside names for pointer, with the key.
func (s Siblings) useOf(pointer jsontext.Pointer) (*sites, string, bool) {
	n := s.at
	for token := range pointer.Tokens() {
		if n == nil {
			break
		}
		if n.ends && s.list[n.model].keys[token] {
			return n, token, true
		}
		n = n.next[token]
	}
	return nil, "", false
}

// Writes reports whether obj, a reference object, is written as the mapping
// the fields beside a path item $ref write at pointer: the field itself, held
// or copied, rather than one the $ref names. With external references allowed,
// the resolver can answer a pointer into those fields so, since the load phase
// holds the source for it (external.hold). A pointer through an alias's
// position reads the fields where their model was found. No schema is written.
func (s Siblings) Writes(pointer jsontext.Pointer, obj any) bool {
	n, _, beside := s.useOf(pointer)
	if !beside {
		return false
	}
	node, ok := s.writtenAs[s.list[n.model].Pointer+pointer[len(n.use):]]
	return ok && nodeview.Deref(ownRoot(obj)) == node
}

// useSite returns the mapping rp, which must not be nil, is written as, through
// any alias, or nil when rp has no node.
func useSite(rp *soa.ReferencedPathItem) *yaml.Node {
	return nodeview.Deref(rp.GetCore().GetRootNode())
}

// siblingBuild models the fields of one use site, or reports why it could not.
type siblingBuild func(ctx context.Context, node *yaml.Node) (*soa.PathItem, error)

// modelSiblings models the fields beside every path item $ref the walk of doc
// reaches, and beside every one inside what it models, returning an error
// diagnostic for each use site the parser faulted on.
//
// A tree in which no mapping keys $ref beside another key holds no use site and
// is not walked: that is nearly every document, and the walk is a pass over the
// whole model.
func modelSiblings(ctx context.Context, doc *soa.OpenAPI, locate scan.Locator) (Siblings, []ir.Diagnostic) {
	if !anyNode(doc.GetRootNode(), refBeside) {
		return Siblings{}, nil
	}
	return siblingsFrom(ctx, soa.Walk(ctx, doc), locate, buildSibling)
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

// siblingsFrom is modelSiblings over a walk and a model build the caller
// supplies, so a test can hand it a walk that panics, or a build that faults.
//
// The doc's walk comes first, then each model's in the order it was found, so
// a model found inside another is walked too. Each mapping is modelled once,
// so there are no more models than mappings the source holds, which its node
// bound caps. A walk that panics stops the search where it stands; the
// resolution walk meets the same fault at the same model, and reports it.
func siblingsFrom(ctx context.Context, items iter.Seq[soa.WalkItem], locate scan.Locator, build siblingBuild,
) (Siblings, []ir.Diagnostic) {
	s := Siblings{byNode: map[*yaml.Node]int{}, faulted: map[*yaml.Node]bool{}, at: &sites{},
		writtenAs: map[jsontext.Pointer]*yaml.Node{}}
	var faults []ir.Diagnostic
	met := func(site jsontext.Pointer, rp *soa.ReferencedPathItem) {
		faults = append(faults, s.add(ctx, site, rp, build, locate)...)
	}
	// A model's walk also records what each reference object in it is written
	// as; the document's matches its path items alone.
	inModel := func(site jsontext.Pointer, w referenceWrapper) error {
		if _, schema := w.(*schemaRef); !schema {
			s.write(site, w)
		}
		if rp, isItem := w.(*soa.ReferencedPathItem); isItem {
			met(site, rp)
		}
		return nil
	}
	_, err := eachModel(items, "sibling search", func(site jsontext.Pointer, rp *soa.ReferencedPathItem) error {
		met(site, rp)
		return nil
	})
	// The document's walk, then each model's in turn, until one panics.
	for i := 0; err == nil && i < len(s.list) && i < sourceindex.MaxIndexedNodes; i++ {
		_, err = eachModel(s.modelWalk(ctx, i), "sibling search", inModel)
	}
	return s, faults
}

// write records the mapping w, met at site in a model's walk, is written as.
// One with none is not recorded, so no answer without one is taken for it.
func (s *Siblings) write(site jsontext.Pointer, w referenceWrapper) {
	if node := nodeview.Deref(ownRoot(w)); node != nil {
		s.writtenAs[site] = node
	}
}

// add models the fields rp's use site writes beside its $ref, at site. It does
// nothing for an rp that is no reference, nor for a mapping holding nothing
// beside the $ref; a mapping already met is only noted at site. A mapping the
// parser faulted on holds no model, and its fault is returned as an error
// diagnostic instead.
func (s *Siblings) add(ctx context.Context, site jsontext.Pointer, rp *soa.ReferencedPathItem, build siblingBuild,
	locate scan.Locator,
) []ir.Diagnostic {
	if !rp.IsReference() {
		return nil
	}
	node := useSite(rp)
	if s.faulted[node] {
		return nil
	}
	if i, seen := s.byNode[node]; seen {
		s.at.insert(site, i)
		return nil
	}
	fields, ok := withoutRef(node)
	if !ok {
		return nil
	}
	item, err := build(ctx, fields)
	if err != nil {
		s.faulted[node] = true
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, locate(node),
			"the fields written beside this path item's $ref could not be read: %s", err.Error())}
	}
	s.byNode[node] = len(s.list)
	s.at.insert(site, len(s.list))
	s.list = append(s.list, Sibling{Item: item, Pointer: site, keys: writtenKeys(fields)})
	return nil
}

// writtenKeys returns the keys the mapping fields, a use site less its $ref, is
// written under, read as the parser reads it.
func writtenKeys(fields *yaml.Node) map[string]bool {
	pairs := nodeview.Pairs(fields)
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
// the library reads a mapping's key (nodeview.KeyOf).
//
// It and the readers above take the pairs a mapping writes itself, not those
// a `<<` merges in: the library tells a reference by its own pairs, so a $ref
// merged in makes no reference, while a `<<` beside an own $ref is a field
// the decoder of the copy merges (TestWithoutRef_DropsTheRefAsTheLibraryReadsIt).
func isRefKey(key *yaml.Node) bool {
	k, own := nodeview.KeyOf(key)
	return own && k == refKey
}

// buildSibling builds the fields beside a $ref into a Path Item, as the source's
// own path items are built. Nothing is validated: the table judges them as the
// item's own fields.
func buildSibling(ctx context.Context, node *yaml.Node) (item *soa.PathItem, err error) {
	defer recovered(&err, "parser")
	item = soa.NewPathItem()
	_, err = marshaller.UnmarshalNode(ctx, "", node, item)
	return item, err
}
