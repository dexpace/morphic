package operation

import (
	"slices"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// groupSpec is everything that identifies one operation group before any
// operation lands in it. Each field is a function of the key alone, so the group
// an operation creates does not depend on which operation arrived first.
type groupSpec struct {
	// key merges operations into one group; it is the compiler's own and never
	// reaches the IR.
	key string
	// id, name, docs and prov become the group's own fields.
	id   ir.GroupID
	name ir.Naming
	docs ir.Docs
	prov ir.Provenance
	// inferred marks the operations the strategy placed by a heuristic.
	inferred string
	// synth is true for a group no tag declares, whose hint finalize mints so
	// it cannot spell what a declared group is already called.
	synth bool
	// base is the hint a synthesized group asks for before collision avoidance.
	base string
}

// serviceGroups accumulates operation groups keyed by a namespaced key while
// preserving first-seen insertion order, so a group's operations gather across
// paths without reordering the groups themselves.
type serviceGroups struct {
	order []string
	byKey map[string]*ir.OperationGroup
	synth map[string]groupSpec
	// declared holds the tag names that name a group under the active strategy.
	declared []string
}

// newServiceGroups returns an empty group accumulator. A declared tag claims its
// spelling only under tag grouping, the one strategy whose groups it names.
func newServiceGroups(c lowering.Ctx) *serviceGroups {
	g := &serviceGroups{
		byKey: make(map[string]*ir.OperationGroup),
		synth: make(map[string]groupSpec),
	}
	if c.Grouping == lowering.GroupByPathPrefix {
		return g
	}
	for _, t := range c.Doc.GetTags() {
		if t != nil {
			g.declared = append(g.declared, t.GetName())
		}
	}
	return g
}

// specFor resolves the group an operation belongs to under the active strategy.
// lowering.GroupByPathPrefix is a heuristic, so it stamps the inferred marker;
// grouping by declared tags is a declared fact and leaves it empty.
func (g *serviceGroups) specFor(c lowering.Ctx, src *soa.Operation, path string) groupSpec {
	if c.Grouping == lowering.GroupByPathPrefix {
		seg := firstPathSegment(path)
		prov := c.ProvenanceAt("")
		prov.Inferred = "group-path-prefix"
		return groupSpec{
			key: "seg:" + seg, id: ids.SynthGroup(ir.SynthRulePathPrefix, seg),
			name: compile.NamingFor(seg), prov: prov, inferred: "group-path-prefix",
		}
	}
	tags := src.GetTags()
	if len(tags) == 0 {
		return g.synthSpec(c, "default", ir.SynthRuleDefault)
	}
	return tagSpec(c, tags[0])
}

// webhookSpec is the spec of the group that holds webhook operations. No
// document declares it: the compiler synthesizes it, and Naming.Source is the
// spelling the source used (GitHub #184).
func (g *serviceGroups) webhookSpec(c lowering.Ctx) groupSpec {
	return g.synthSpec(c, "webhook", ir.SynthRuleWebhooks)
}

// synthSpec builds the spec of a group minted by rule. Its ID lives in the
// synthesized space; its name is a hint finalize settles.
func (g *serviceGroups) synthSpec(c lowering.Ctx, key, rule string) groupSpec {
	prov := c.ProvenanceAt("")
	prov.Inferred = "group-" + rule
	spec := groupSpec{
		key: key, id: ids.SynthGroup(rule), prov: prov,
		synth: true, base: rule,
	}
	g.synth[key] = spec
	return spec
}

// tagSpec builds the spec of the group a tag declares, or that an operation
// uses without a declaration: either way the ID is the tag's name. A declared
// tag's provenance points at its declaration; an undeclared one has no
// declaration, so it records the document root rather than the first operation
// to use it, which would depend on path order.
func tagSpec(c lowering.Ctx, name string) groupSpec {
	spec := groupSpec{
		key: "tag:" + name, id: ids.TagGroup(name), name: compile.NamingFor(name),
		prov: c.ProvenanceAt(""),
	}
	for i, t := range c.Doc.GetTags() {
		if t != nil && t.GetName() == name {
			spec.docs = tagDocsFrom(t)
			spec.prov = c.ProvenanceAt(ids.Ptr("tags", strconv.Itoa(i)))
			break
		}
	}
	return spec
}

// group returns the group for spec, creating it on first sight and recording its
// insertion order.
func (g *serviceGroups) group(spec groupSpec) *ir.OperationGroup {
	if existing, ok := g.byKey[spec.key]; ok {
		return existing
	}
	g.byKey[spec.key] = &ir.OperationGroup{ID: spec.id, Name: spec.name, Docs: spec.docs, Provenance: spec.prov}
	g.order = append(g.order, spec.key)
	return g.byKey[spec.key]
}

// finalize returns the accumulated groups in insertion order, after minting each
// synthesized group a hint that no declared or used group already spells. A name
// is compared as its canonical words, the form an emitter renders from, so
// "Default" and "default" collide as they would once rendered.
func (g *serviceGroups) finalize() []ir.OperationGroup {
	g.settleHints()
	out := make([]ir.OperationGroup, 0, len(g.order))
	for _, k := range g.order {
		out = append(out, *g.byKey[k])
	}
	return out
}

// settleHints names every synthesized group. The keys are visited sorted so the
// result does not follow the order operations arrived in.
func (g *serviceGroups) settleHints() {
	claimed := make(map[string]bool)
	for _, name := range g.declared {
		claimed[ir.CanonicalWords(name)] = true
	}
	for key, grp := range g.byKey {
		if _, minted := g.synth[key]; !minted {
			claimed[ir.CanonicalWords(grp.Name.Source)] = true
		}
	}
	keys := make([]string, 0, len(g.synth))
	for key := range g.synth {
		if _, used := g.byKey[key]; used {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		hint := freeHint(g.synth[key].base, claimed)
		claimed[ir.CanonicalWords(hint)] = true
		g.byKey[key].Name = compile.NamingHint(hint)
	}
}

// freeHint returns base when its words are unclaimed, else the first of
// base_2, base_3, … that is.
func freeHint(base string, claimed map[string]bool) string {
	hint := base
	for n := 2; claimed[ir.CanonicalWords(hint)]; n++ {
		hint = compile.SubHint(base, strconv.Itoa(n))
	}
	return hint
}
