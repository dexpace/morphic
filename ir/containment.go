package ir

// appendContained appends to dst the types a value of td is made of: the types
// of its members and elements, and of what it composes. A discriminator's
// mapping, a default, an example, an encoding and a template argument name
// types too, but none makes a value of td hold a value of another type, so none
// is an edge. An empty target is skipped, and a nil or typed-nil td adds
// nothing. [Facts.Usage] spreads along these edges and [Facts.Recursive] reads
// cycles in them.
func appendContained(dst []TypeID, td TypeDef) []TypeID {
	if IsNilTypeDef(td) {
		return dst
	}
	switch t := td.(type) {
	case *Model:
		return appendModelParts(dst, t)
	case *Union:
		for i := range t.Variants {
			dst = appendTarget(dst, t.Variants[i].Type.Target)
		}
		return dst
	case *List:
		return appendTarget(dst, t.Elem.Target)
	case *MapT:
		return appendTarget(appendRef(dst, t.Key), t.Value.Target)
	case *Tuple:
		return appendRef(appendRefs(dst, t.Elems), t.Rest)
	case *Scalar:
		return appendRef(dst, t.Base)
	default:
		// Primitive, Enum, Literal, External and Any are made of no other type.
		return dst
	}
}

// appendModelParts appends what a model holds: its properties, its catch-all,
// then the types it composes in, whether as Base, Implements or Mixins.
func appendModelParts(dst []TypeID, m *Model) []TypeID {
	for i := range m.Properties {
		dst = appendTarget(dst, m.Properties[i].Type.Target)
	}
	if extra := m.AdditionalProps; extra != nil {
		dst = appendTarget(dst, extra.Value.Target)
		dst = appendRef(dst, extra.Key)
		for i := range extra.Patterns {
			dst = appendTarget(dst, extra.Patterns[i].Value.Target)
		}
	}
	dst = appendRef(dst, m.Base)
	dst = appendRefs(dst, m.Implements)
	return appendRefs(dst, m.Mixins)
}

// appendTarget appends id unless it is empty.
func appendTarget(dst []TypeID, id TypeID) []TypeID {
	if id == "" {
		return dst
	}
	return append(dst, id)
}

// appendRef appends the target of r, if there is a reference at all.
func appendRef(dst []TypeID, r *TypeRef) []TypeID {
	if r == nil {
		return dst
	}
	return appendTarget(dst, r.Target)
}

// appendRefs appends the target of each reference in rs.
func appendRefs(dst []TypeID, rs []TypeRef) []TypeID {
	for i := range rs {
		dst = appendTarget(dst, rs[i].Target)
	}
	return dst
}
