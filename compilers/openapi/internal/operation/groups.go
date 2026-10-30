// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// lowerTagDefs registers the document's declared tag metadata into TagDefs; tag
// membership itself stays as []string on each tagged operation.
//
// Parent and Kind are recorded as declared (OpenAPI 3.2's tag hierarchy and tag
// role, both dropped until GitHub #613). Reading a role out of Kind is grouping
// policy rather than a property of the document, so it is not interpreted here.
func lowerTagDefs(c lowering.Ctx) []ir.TagDef {
	tags := c.Doc.GetTags()
	if len(tags) == 0 {
		return nil
	}
	defs := make([]ir.TagDef, 0, len(tags))
	for _, t := range tags {
		if t == nil {
			continue
		}
		defs = append(defs, ir.TagDef{
			Name:   t.GetName(),
			Docs:   tagDocsFrom(t),
			Parent: t.GetParent(),
			Kind:   t.GetKind(),
		})
	}
	return defs
}

// tagDocsFrom maps a Tag's summary, description, and externalDocs onto Docs.
func tagDocsFrom(t *soa.Tag) ir.Docs {
	d := ir.Docs{Summary: t.GetSummary(), Description: t.GetDescription()}
	if ed := t.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
	return d
}

// groupFor resolves the groups an operation is filed under by the active
// strategy, outermost first, by identity: the ID is both the group's key here
// and its ID in the IR, so two operations share a group exactly when they share
// an ID. The untagged group's hint stays "default" beside a tag of that name:
// the IDs differ, and the emitter resolves name collisions by ID
// (docs/emitters/refine/naming.md). By tags, the group is the first
// navigational tag's, nested under the tags its declared parents name (GitHub
// #613).
func (g *serviceGroups) groupFor(c lowering.Ctx, src *soa.Operation, path string) []groupLevel {
	if c.Grouping == lowering.GroupByPathPrefix {
		seg := firstPathSegment(path)
		return []groupLevel{{id: ids.PathPrefixGroup(seg), name: compile.NamingFor(seg)}}
	}
	for _, tag := range src.GetTags() {
		if g.tags.navigational(tag) {
			return g.tags.levels(tag)
		}
	}
	return []groupLevel{{id: ids.DefaultGroup(), name: compile.NamingHint("default")}}
}

// groupLevel is one group an operation is filed under: its ID, and the naming
// and docs it is created with.
type groupLevel struct {
	id   ir.GroupID
	name ir.Naming
	docs ir.Docs
}

// navigationalKind is the tag kind OpenAPI 3.2's registry defines for a tag
// that groups operations into sections.
const navigationalKind = "nav"

// tagIndex is a document's declared tags by name, the first declaration of a
// name winning, so which one supplies a tag's kind, parent and docs does not
// depend on the order operations are lowered in.
type tagIndex map[string]*soa.Tag

// indexTags indexes tags by name.
func indexTags(tags []*soa.Tag) tagIndex {
	out := make(tagIndex, len(tags))
	for _, t := range tags {
		if _, seen := out[t.GetName()]; t != nil && !seen {
			out[t.GetName()] = t
		}
	}
	return out
}

// navigational reports whether tag groups operations into sections: one the
// document does not declare, or one declaring no kind or the navigational one.
// Any other kind is skipped, registered or not, as reading a section out of an
// unregistered string would be an inference (invariant 6). Nothing is reported:
// TagDef keeps the kind and Operation.Tags every membership.
func (x tagIndex) navigational(tag string) bool {
	kind := x[tag].GetKind()
	return kind == "" || kind == navigationalKind
}

// parent returns the tag tag nests under: the parent it declares, when that is
// itself a declared navigational tag, and "" otherwise.
func (x tagIndex) parent(tag string) string {
	parent := x[tag].GetParent()
	if parent == "" || x[parent] == nil || !x.navigational(parent) {
		return ""
	}
	return parent
}

// chain returns the tags tag nests under, outermost first and ending with tag,
// following each declared parent. Where the walk meets a tag it already passed,
// the parents form a cycle: the chain starts at the first tag of the cycle it
// reached, so the tags on a cycle nest under none of one another while every
// tag outside it keeps its parent. Any suffix of a walk is the walk from that
// tag, so every tag's chain agrees with its descendants'. A walk passes each
// declared tag at most once, which bounds it.
func (x tagIndex) chain(tag string) []string {
	walk := []string{tag}
	at := map[string]int{tag: 0}
	for range len(x) {
		parent := x.parent(walk[len(walk)-1])
		if parent == "" {
			break
		}
		if i, seen := at[parent]; seen {
			walk = walk[:i+1]
			break
		}
		at[parent] = len(walk)
		walk = append(walk, parent)
	}
	slices.Reverse(walk)
	return walk
}

// levels returns the groups an operation filed under tag nests in, outermost
// first.
func (x tagIndex) levels(tag string) []groupLevel {
	chain := x.chain(tag)
	out := make([]groupLevel, 0, len(chain))
	for _, t := range chain {
		out = append(out, groupLevel{id: ids.TagGroup(t), name: compile.NamingFor(t), docs: tagDocsFrom(x[t])})
	}
	return out
}

// groupingInferred is the inferred marker groupFor's strategy stamps on a path
// operation: lowering.GroupByPathPrefix is a heuristic, so it stamps one, and
// grouping by declared tags is a declared fact, so it stamps none.
func groupingInferred(c lowering.Ctx) ir.Inferred {
	if c.Grouping == lowering.GroupByPathPrefix {
		return lowering.GroupPathPrefixHeuristic
	}
	return ""
}

// firstPathSegment returns the first non-empty segment of a path, or "" when the
// path has none.
func firstPathSegment(path string) string {
	for seg := range strings.SplitSeq(path, "/") {
		if seg != "" {
			return seg
		}
	}
	return ""
}

// serviceGroups accumulates operation groups by ID while preserving first-seen
// insertion order, so a group's operations gather across paths without
// reordering the groups themselves. A group nested under another (an OpenAPI
// 3.2 tag parent) is recorded in parentOf and attached to it by finalize.
type serviceGroups struct {
	tags     tagIndex
	order    []ir.GroupID
	byID     map[ir.GroupID]*ir.OperationGroup
	parentOf map[ir.GroupID]ir.GroupID
}

// newServiceGroups returns an empty group accumulator over the declared tags.
func newServiceGroups(tags []*soa.Tag) *serviceGroups {
	return &serviceGroups{
		tags:     indexTags(tags),
		byID:     make(map[ir.GroupID]*ir.OperationGroup),
		parentOf: make(map[ir.GroupID]ir.GroupID),
	}
}

// group returns the group with the given ID, creating it via mk on first sight
// and recording its insertion order. It stamps the ID itself, so a group's ID is
// its key by construction rather than by every caller repeating it.
func (g *serviceGroups) group(id ir.GroupID, mk func() ir.OperationGroup) *ir.OperationGroup {
	if existing, ok := g.byID[id]; ok {
		return existing
	}
	created := mk()
	created.ID = id
	g.byID[id] = &created
	g.order = append(g.order, id)
	return &created
}

// place returns the innermost group of levels, creating each level on first
// sight nested under the one before it. A level's parent is fixed when it is
// created, which is sound because every operation reaching a group reaches it
// through the same outer levels (tagIndex.chain), and it puts each parent
// before its children in the insertion order.
func (g *serviceGroups) place(levels []groupLevel) *ir.OperationGroup {
	var grp *ir.OperationGroup
	var parent ir.GroupID
	for _, l := range levels {
		if _, seen := g.byID[l.id]; !seen && parent != "" {
			g.parentOf[l.id] = parent
		}
		grp = g.group(l.id, func() ir.OperationGroup { return ir.OperationGroup{Name: l.name, Docs: l.docs} })
		parent = l.id
	}
	return grp
}

// finalize returns the accumulated groups in insertion order, each carrying the
// groups nested under it in insertion order, and only those with no parent at
// the top level. It builds from the end of the order, where a parent's children
// are already built, so it needs no recursion.
func (g *serviceGroups) finalize() []ir.OperationGroup {
	children := make(map[ir.GroupID][]ir.GroupID, len(g.parentOf))
	for _, id := range g.order {
		if parent, nested := g.parentOf[id]; nested {
			children[parent] = append(children[parent], id)
		}
	}
	built := make(map[ir.GroupID]ir.OperationGroup, len(g.order))
	for _, id := range slices.Backward(g.order) {
		grp := *g.byID[id]
		for _, child := range children[id] {
			grp.Groups = append(grp.Groups, built[child])
		}
		built[id] = grp
	}
	out := make([]ir.OperationGroup, 0, len(g.order))
	for _, id := range g.order {
		if _, nested := g.parentOf[id]; !nested {
			out = append(out, built[id])
		}
	}
	return out
}
