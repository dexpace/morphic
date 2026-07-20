package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// kindHeader peeks the adjacent tag during decoding.
type kindHeader struct {
	Kind TypeKind `json:"kind"`
}

// marshalWithKind emits {"kind":"<k>", ...fields of v...}. The value v must be
// an alias of a concrete TypeDef kind so json.Marshal encodes it as an object
// without re-entering the kind's own MarshalJSON.
func marshalWithKind(k TypeKind, v any) ([]byte, error) {
	fields, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ir: marshal %s: %w", k, err)
	}
	if len(fields) < 2 || fields[0] != '{' {
		return nil, fmt.Errorf("ir: marshal %s: concrete kind must encode as an object", k)
	}
	var buf bytes.Buffer
	buf.Grow(len(fields) + 16)
	fmt.Fprintf(&buf, `{"kind":%q`, string(k))
	if !bytes.Equal(fields, []byte("{}")) {
		buf.WriteByte(',')
		buf.Write(fields[1 : len(fields)-1])
		buf.WriteByte('}')
		return buf.Bytes(), nil
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// MarshalJSON encodes the Primitive with an adjacent "kind" tag.
func (p *Primitive) MarshalJSON() ([]byte, error) {
	type alias Primitive
	return marshalWithKind(KindPrimitive, (*alias)(p))
}

// MarshalJSON encodes the Scalar with an adjacent "kind" tag.
func (s *Scalar) MarshalJSON() ([]byte, error) {
	type alias Scalar
	return marshalWithKind(KindScalar, (*alias)(s))
}

// MarshalJSON encodes the Model with an adjacent "kind" tag.
func (m *Model) MarshalJSON() ([]byte, error) {
	type alias Model
	return marshalWithKind(KindModel, (*alias)(m))
}

// MarshalJSON encodes the Union with an adjacent "kind" tag.
func (u *Union) MarshalJSON() ([]byte, error) {
	type alias Union
	return marshalWithKind(KindUnion, (*alias)(u))
}

// MarshalJSON encodes the Enum with an adjacent "kind" tag.
func (e *Enum) MarshalJSON() ([]byte, error) {
	type alias Enum
	return marshalWithKind(KindEnum, (*alias)(e))
}

// MarshalJSON encodes the List with an adjacent "kind" tag.
func (l *List) MarshalJSON() ([]byte, error) {
	type alias List
	return marshalWithKind(KindList, (*alias)(l))
}

// MarshalJSON encodes the MapT with an adjacent "kind" tag.
func (m *MapT) MarshalJSON() ([]byte, error) {
	type alias MapT
	return marshalWithKind(KindMap, (*alias)(m))
}

// MarshalJSON encodes the Tuple with an adjacent "kind" tag.
func (t *Tuple) MarshalJSON() ([]byte, error) {
	type alias Tuple
	return marshalWithKind(KindTuple, (*alias)(t))
}

// MarshalJSON encodes the Literal with an adjacent "kind" tag.
func (l *Literal) MarshalJSON() ([]byte, error) {
	type alias Literal
	return marshalWithKind(KindLiteral, (*alias)(l))
}

// MarshalJSON encodes the External with an adjacent "kind" tag.
func (e *External) MarshalJSON() ([]byte, error) {
	type alias External
	return marshalWithKind(KindExternal, (*alias)(e))
}

// MarshalJSON encodes the Any with an adjacent "kind" tag.
func (a *Any) MarshalJSON() ([]byte, error) {
	type alias Any
	return marshalWithKind(KindAny, (*alias)(a))
}

// UnmarshalJSON decodes a kind-tagged TypeDef per entry, dispatching through
// the same registry the completeness test walks.
func (r *TypeRegistry) UnmarshalJSON(data []byte) error {
	var rawByID map[TypeID]json.RawMessage
	if err := json.Unmarshal(data, &rawByID); err != nil {
		return fmt.Errorf("ir: type registry: %w", err)
	}
	out := make(TypeRegistry, len(rawByID))
	for id, raw := range rawByID {
		var head kindHeader
		if err := json.Unmarshal(raw, &head); err != nil {
			return fmt.Errorf("ir: type %s: reading kind tag: %w", id, err)
		}
		td, ok := NewTypeDef(head.Kind)
		if !ok {
			return fmt.Errorf("ir: type %s: unknown kind %q", id, head.Kind)
		}
		if err := json.Unmarshal(raw, td); err != nil {
			return fmt.Errorf("ir: type %s (%s): %w", id, head.Kind, err)
		}
		out[id] = td
	}
	*r = out
	return nil
}
