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
		if primitive, ok := td.(*ir.Primitive); ok {
			c.offerType(segment("prim", string(primitive.PrimitiveKind)), id, td)
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
	c.offer(key, string(id), func(correlatorValue *correlator, key string) { correlatorValue.typeParts(key, td) })
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
		c.ref(part(key, "elem"), &t.ElementType)
		c.decoded(key, t.Encoding)
	case *ir.MapT:
		c.ref(part(key, "key"), t.Key)
		c.ref(part(key, "value"), &t.Value)
	case *ir.Tuple:
		for i := range t.ElementTypes {
			c.ref(child(key, "elem", strconv.Itoa(i)), &t.ElementTypes[i])
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
func (c *correlator) decoded(key string, encoding *ir.Encoding) {
	if encoding != nil {
		c.ref(part(key, "decoded"), encoding.Schema)
	}
}

// model offers the keys of irModel's members. The anonymous models irModel
// composes in place are part of it, so their members are keyed as irModel's
// own, each wire name keying the declaration irModel's flattening takes and
// none it overrides, and each such model itself is keyed by irModel's key and
// allOf. Every catch-all among them is keyed as irModel's.
func (c *correlator) model(key string, irModel *ir.Model) {
	own := c.composedInPlace(irModel)
	flat := ir.FlattenedProperties(c.doc.Types, irModel.ID)
	for i := range flat.Properties {
		if decl := flat.Properties[i].PropertyDecl; own[decl.Owner] != nil {
			c.property(key, decl.Property)
		}
	}
	for id, model := range own {
		c.additional(key, model.AdditionalProperties)
		if id != irModel.ID {
			c.offerType(part(key, "allOf"), id, model)
		}
	}
}

// composedInPlace returns model and every anonymous model it composes in place,
// directly or through another such model, by ID.
func (c *correlator) composedInPlace(model *ir.Model) map[ir.TypeID]*ir.Model {
	own := map[ir.TypeID]*ir.Model{model.ID: model}
	queue := []*ir.Model{model}
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

// composed lists what model composes: its Base, Implements and Mixins.
func composed(model *ir.Model) []ir.TypeRef {
	refs := slices.Concat(model.Implements, model.Mixins)
	if model.Base != nil {
		refs = append(refs, *model.Base)
	}
	return refs
}

// inlineConjuncts returns every anonymous model another model composes: an
// allOf member written in place, whose members are the composing model's.
func inlineConjuncts(types ir.TypeRegistry) map[ir.TypeID]bool {
	inline := map[ir.TypeID]bool{}
	for _, td := range types {
		model, ok := td.(*ir.Model)
		if !ok || model == nil {
			continue
		}
		for _, ref := range composed(model) {
			target, ok := types[ref.Target].(*ir.Model)
			if ok && target != nil && target.Anonymous {
				inline[ref.Target] = true
			}
		}
	}
	return inline
}

// members offers each member of enum its key, key then the member's key, which
// its ID holds beneath the enum's namespace and path (docs/ir/identity.md#enum-members).
func (c *correlator) members(key string, enum *ir.Enum) {
	scope, ok := strings.CutPrefix(string(enum.ID), ir.IDKindType+ir.IDSeparator)
	if !ok {
		return
	}
	prefix := ir.IDKindEnumMember + ir.IDSeparator + scope + ir.IDSeparator
	for i := range enum.Members {
		id := string(enum.Members[i].ID)
		if rest, ok := strings.CutPrefix(id, prefix); ok {
			c.offer(child(key, "member", rest), id, nil)
		}
	}
}

// property offers property its key, and, when property takes it, keys
// property's type, what its encoding decodes to, the headers of a multipart
// part encoding it and its field arguments.
func (c *correlator) property(key string, property *ir.Property) {
	c.offer(child(key, "prop", property.EffectiveWireName()), string(property.ID), func(correlatorValue *correlator, key string) {
		correlatorValue.ref(part(key, "type"), &property.Type)
		correlatorValue.decoded(key, property.Encoding)
		correlatorValue.headers(key, correlatorValue.partHeaders[property.ID])
		for i := range property.Args {
			arg := &property.Args[i]
			correlatorValue.offer(child(key, "arg", arg.Name.Source), string(arg.ID), func(otherCorrelator *correlator, key string) {
				otherCorrelator.ref(part(key, "schema"), &arg.Type)
				otherCorrelator.decoded(key, arg.Encoding)
			})
		}
	})
}

// additional keys the types a model's catch-all names: its value, its key and
// each pattern's value.
func (c *correlator) additional(key string, extra *ir.AdditionalProperties) {
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
func (c *correlator) variants(key string, union *ir.Union) {
	for i := range union.Variants {
		variant := &union.Variants[i]
		if seg := variantSegment(union, variant); seg != "" {
			c.ref(key+"/"+seg, &variant.Type)
		}
	}
}

// variantSegment is the segment naming variant in union, "" when nothing does.
func variantSegment(union *ir.Union, variant *ir.Variant) string {
	if union.WireTagged && union.Discriminator != nil {
		var tags []string
		for tag, target := range union.Discriminator.Mapping {
			if target == variant.Type.Target {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			return segment("tag", slices.Min(tags))
		}
	}
	if union.WireTagged && union.Discriminator == nil {
		wire := variant.WireName
		if wire == "" {
			wire = variant.Name.Source
		}
		if wire != "" {
			return segment("tag", wire)
		}
	}
	return named("variant", variant.Name)
}
