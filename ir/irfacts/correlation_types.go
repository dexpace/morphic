// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// seedTypes offers each named type its key: its namespace and declared name, a
// primitive its kind. An anonymous type is keyed only by the positions that
// name it.
func (c *correlator) seedTypes() {
	for id, td := range c.doc.Types {
		if ir.IsNilTypeDef(td) {
			continue
		}
		if prim, ok := td.(*ir.Primitive); ok {
			c.offerType(segment("prim", string(prim.Prim)), id, td)
			continue
		}
		common := td.Common()
		if common.Anonymous {
			continue
		}
		if key := named("type", common.Name); key != "" {
			c.offerType(namespaced(common.Namespace, key), id, td)
		}
	}
}

// namespaced puts each segment of a namespace before key.
func namespaced(namespace []string, key string) string {
	for _, part := range slices.Backward(namespace) {
		key = segment("ns", part) + "/" + key
	}
	return key
}

// offerType offers key to the type id, and, when it takes it, keys what td
// names.
func (c *correlator) offerType(key string, id ir.TypeID, td ir.TypeDef) {
	c.offer(key, string(id), func(c *correlator, key string) { c.typeParts(key, td) })
}

// ref offers key to the type ref names, when the type is anonymous: a named
// type is keyed by its name wherever it is used.
func (c *correlator) ref(key string, ref *ir.TypeRef) {
	if ref == nil {
		return
	}
	td, ok := c.doc.Types[ref.Target]
	if !ok || ir.IsNilTypeDef(td) || !td.Common().Anonymous {
		return
	}
	c.offerType(key, ref.Target, td)
}

// typeParts offers the keys of what td, keyed key, names: a model's members, a
// union's tagged or named variants, an enum's members, a container's elements
// and a scalar's base, and what an encoding decodes to.
func (c *correlator) typeParts(key string, td ir.TypeDef) {
	switch t := td.(type) {
	case *ir.Model:
		if !c.inline[t.ID] {
			c.model(key, t)
		}
	case *ir.Union:
		c.variants(key, t)
	case *ir.Enum:
		c.members(key, t)
	case *ir.List:
		c.ref(part(key, "elem"), &t.Elem)
		c.decoded(key, t.Encoding)
	case *ir.MapT:
		c.ref(part(key, "key"), t.Key)
		c.ref(part(key, "value"), &t.Value)
	case *ir.Tuple:
		for i := range t.Elems {
			c.ref(child(key, "elem", strconv.Itoa(i)), &t.Elems[i])
		}
		c.ref(part(key, "rest"), t.Rest)
	case *ir.Scalar:
		c.ref(part(key, "base"), t.Base)
		c.decoded(key, t.Encoding)
	default:
		// Primitive, Literal, External and Any name nothing.
	}
}

// decoded offers the type an encoding says a value holds once decoded.
func (c *correlator) decoded(key string, e *ir.Encoding) {
	if e != nil {
		c.ref(part(key, "decoded"), e.Schema)
	}
}

// model offers the keys of m's members. The anonymous models m composes in
// place are part of it, so their members are keyed as m's own, each wire name
// keying the declaration m's flattening takes and none it overrides, and each
// such model itself is keyed by m's key and allOf. Every catch-all among them
// is keyed as m's.
func (c *correlator) model(key string, m *ir.Model) {
	own := c.composedInPlace(m)
	flat := ir.FlattenedProperties(c.doc.Types, m.ID)
	for i := range flat.Properties {
		if decl := flat.Properties[i].PropertyDecl; own[decl.Owner] != nil {
			c.property(key, decl.Property)
		}
	}
	for id, model := range own {
		c.additional(key, model.AdditionalProps)
		if id != m.ID {
			c.offerType(part(key, "allOf"), id, model)
		}
	}
}

// composedInPlace returns m and every anonymous model it composes in place,
// directly or through another such model, by ID.
func (c *correlator) composedInPlace(m *ir.Model) map[ir.TypeID]*ir.Model {
	own := map[ir.TypeID]*ir.Model{m.ID: m}
	queue := []*ir.Model{m}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, ref := range composed(next) {
			conjunct, ok := c.doc.Types[ref.Target].(*ir.Model)
			if !ok || !c.inline[ref.Target] || own[ref.Target] != nil {
				continue
			}
			own[ref.Target] = conjunct
			queue = append(queue, conjunct)
		}
	}
	return own
}

// composed lists what m composes: its Base, Implements and Mixins.
func composed(m *ir.Model) []ir.TypeRef {
	refs := slices.Concat(m.Implements, m.Mixins)
	if m.Base != nil {
		refs = append(refs, *m.Base)
	}
	return refs
}

// inlineConjuncts returns every anonymous model another model composes: an
// allOf member written in place, whose members are the composing model's.
func inlineConjuncts(types ir.TypeRegistry) map[ir.TypeID]bool {
	inline := map[ir.TypeID]bool{}
	for _, td := range types {
		m, ok := td.(*ir.Model)
		if !ok || m == nil {
			continue
		}
		for _, ref := range composed(m) {
			target, ok := types[ref.Target].(*ir.Model)
			if ok && target != nil && target.Anonymous {
				inline[ref.Target] = true
			}
		}
	}
	return inline
}

// members offers each member of e its key, key then the member's key, which
// its ID holds after the enum's own path.
func (c *correlator) members(key string, e *ir.Enum) {
	prefix := "e/" + strings.TrimPrefix(string(e.ID), "t/") + "/"
	for i := range e.Members {
		id := string(e.Members[i].ID)
		if rest, ok := strings.CutPrefix(id, prefix); ok {
			c.offer(child(key, "member", rest), id, nil)
		}
	}
}

// property offers p its key, and, when p takes it, keys p's type, what its
// encoding decodes to and its field arguments.
func (c *correlator) property(key string, p *ir.Property) {
	c.offer(child(key, "prop", p.EffectiveWireName()), string(p.ID), func(c *correlator, key string) {
		c.ref(part(key, "type"), &p.Type)
		c.decoded(key, p.Encoding)
		for i := range p.Args {
			arg := &p.Args[i]
			c.offer(child(key, "arg", arg.Name.Source), string(arg.ID), func(c *correlator, key string) {
				c.ref(part(key, "schema"), &arg.Type)
			})
		}
	})
}

// additional keys the types a model's catch-all names: its value, its key and
// each pattern's value.
func (c *correlator) additional(key string, extra *ir.AdditionalProps) {
	if extra == nil {
		return
	}
	c.ref(part(key, "additional"), extra.Value)
	c.ref(part(key, "additionalKey"), extra.Key)
	for i := range extra.Patterns {
		c.ref(child(key, "pattern", extra.Patterns[i].Pattern), &extra.Patterns[i].Value)
	}
}

// variants offers each anonymous variant's type the key of what names the
// variant: the tag a discriminator maps to it, the least where several do, or
// its wire name in a key-tagged union, else its declared name. A variant with
// none of these keys nothing.
func (c *correlator) variants(key string, u *ir.Union) {
	for i := range u.Variants {
		v := &u.Variants[i]
		if seg := variantSegment(u, v); seg != "" {
			c.ref(key+"/"+seg, &v.Type)
		}
	}
}

// variantSegment is the segment naming v in u, "" when nothing does.
func variantSegment(u *ir.Union, v *ir.Variant) string {
	if u.WireTagged && u.Discriminator != nil {
		var tags []string
		for tag, target := range u.Discriminator.Mapping {
			if target == v.Type.Target {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			return segment("tag", slices.Min(tags))
		}
	}
	if u.WireTagged && u.Discriminator == nil {
		wire := v.WireName
		if wire == "" {
			wire = v.Name.Source
		}
		if wire != "" {
			return segment("tag", wire)
		}
	}
	return named("variant", v.Name)
}
