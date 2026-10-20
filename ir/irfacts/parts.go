package irfacts

import "github.com/dexpace/morphic/ir"

// typeParts is what one type names, by what each named type is to a value of it
// (ir-design §4.9): held, the types of the values it holds; composed, those
// whose members it includes; alias, the type a scalar stands for; and args, its
// properties' field arguments, which a client writes to select a field.
//
// A discriminator, a default or example value, an encoding, a template
// argument and an availability record name types too, but none puts a value of
// that type in a value of this one, so none is a part.
type typeParts struct {
	held     []ir.TypeID
	composed []ir.TypeID
	alias    ir.TypeID
	args     []ir.TypeID
}

// read replaces p with the parts of td, keeping the storage of its slices. A nil
// or typed-nil td has none.
func (p *typeParts) read(td ir.TypeDef) {
	p.held, p.composed, p.args, p.alias = p.held[:0], p.composed[:0], p.args[:0], ""
	if ir.IsNilTypeDef(td) {
		return
	}
	switch t := td.(type) {
	case *ir.Model:
		p.model(t)
	case *ir.Union:
		for i := range t.Variants {
			p.held = appendTarget(p.held, t.Variants[i].Type.Target)
		}
	case *ir.List:
		p.held = appendTarget(p.held, t.Elem.Target)
	case *ir.MapT:
		p.held = appendTarget(appendRef(p.held, t.Key), t.Value.Target)
	case *ir.Tuple:
		p.held = appendRef(appendRefs(p.held, t.Elems), t.Rest)
	case *ir.Scalar:
		if t.Base != nil {
			p.alias = t.Base.Target
		}
	default:
		// Primitive, Enum, Literal, External and Any name no type a value holds.
	}
}

// model reads a model's parts: its properties and their field arguments, its
// catch-all's value, key and patterns, and its Base, Implements and Mixins.
func (p *typeParts) model(m *ir.Model) {
	for i := range m.Properties {
		prop := &m.Properties[i]
		p.held = appendTarget(p.held, prop.Type.Target)
		for j := range prop.Args {
			p.args = appendTarget(p.args, prop.Args[j].Type.Target)
		}
	}
	if extra := m.AdditionalProps; extra != nil {
		p.held = appendRef(appendTarget(p.held, extra.Value.Target), extra.Key)
		for i := range extra.Patterns {
			p.held = appendTarget(p.held, extra.Patterns[i].Value.Target)
		}
	}
	p.composed = appendRefs(appendRef(p.composed, m.Base), m.Implements)
	p.composed = appendRefs(p.composed, m.Mixins)
}

// appendTarget appends id unless it is empty, which names no type.
func appendTarget(dst []ir.TypeID, id ir.TypeID) []ir.TypeID {
	if id == "" {
		return dst
	}
	return append(dst, id)
}

// appendRef appends the target of r, if there is a reference at all.
func appendRef(dst []ir.TypeID, r *ir.TypeRef) []ir.TypeID {
	if r == nil {
		return dst
	}
	return appendTarget(dst, r.Target)
}

// appendRefs appends the target of each reference in rs.
func appendRefs(dst []ir.TypeID, rs []ir.TypeRef) []ir.TypeID {
	for i := range rs {
		dst = appendTarget(dst, rs[i].Target)
	}
	return dst
}
