package ir

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
)

// The IR's JSON goes through encoding/json/v2 alone. A Document owns its bytes
// in both directions: it pins the options that decide what it writes and what
// it accepts, so neither depends on the options, or the JSON API, a caller
// used.

var (
	// ErrVersionAbsent reports a document that declares no irVersion: a
	// producer that never stamped it (ir-design §2.1).
	ErrVersionAbsent = errors.New("no irVersion")
	// ErrVersionIncompatible reports a document stamped with an irVersion
	// CompatibleVersion rejects: another generation's document.
	ErrVersionIncompatible = errors.New("incompatible irVersion")
)

// MarshalJSONTo encodes the Document under canonicalOptions, whatever options
// the caller passed. Only whitespace, such as indentation, stays the caller's.
// The Document is the unit this holds for (invariant 7): a sub-structure encoded
// on its own takes the caller's options, so sorting its maps takes
// json.Deterministic(true).
func (d *Document) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Document
	err := json.MarshalEncode(enc, (*alias)(d), canonicalOptions())
	return named[Document](err, reflect.TypeFor[alias]())
}

// canonicalOptions fixes every choice but whitespace that decides a document's
// bytes (invariant 7): map entries sorted by key, the minimal string escaping of RFC 8785, and a
// refusal to write a string that is not UTF-8 or an object that repeats a name,
// where the alternatives are a silent U+FFFD and an ambiguous document. The rest
// pin v2's defaults, so no caller option can stringify a number, drop a zero
// struct, respell or reorder a raw value, substitute a marshaler, or write null
// where the IR spells absence by omission.
func canonicalOptions() json.Options {
	return json.JoinOptions(
		json.Deterministic(true),
		json.FormatNilSliceAsNull(false),
		json.FormatNilMapAsNull(false),
		json.OmitZeroStructFields(false),
		json.StringifyNumbers(false),
		json.WithMarshalers(nil),
		jsontext.EscapeForHTML(false),
		jsontext.EscapeForJS(false),
		jsontext.AllowInvalidUTF8(false),
		jsontext.AllowDuplicateNames(false),
		jsontext.CanonicalizeRawInts(false),
		jsontext.CanonicalizeRawFloats(false),
		jsontext.ReorderRawObjects(false),
		jsontext.PreserveRawStrings(false),
	)
}

// UnmarshalJSONFrom decodes a Document this build can read and refuses any
// other before interpreting a member of it (ir-design §2.1): a missing or
// foreign irVersion, a member the schema does not define, a duplicate name, or
// a string that is not UTF-8. The refusals do not depend on the caller's
// options, because the document is decoded again here under the IR's own.
func (d *Document) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}
	if err := checkVersionOf(raw); err != nil {
		return err
	}
	type alias Document
	err = json.Unmarshal(raw, (*alias)(d), json.RejectUnknownMembers(true))
	return named[Document](err, reflect.TypeFor[alias]())
}

// checkVersionOf reads a document's irVersion before anything else in it, so a
// document from another generation is refused as one, not for whichever of its
// members this build fails to recognize first.
func checkVersionOf(raw jsontext.Value) error {
	v, found, err := stringMember(raw, "irVersion")
	switch {
	case err != nil:
		return fmt.Errorf("ir: document: %w", err)
	case !found:
		return fmt.Errorf("ir: this build reads irVersion %s: %w", IRVersion, ErrVersionAbsent)
	case !CompatibleVersion(v):
		return fmt.Errorf("ir: document declares irVersion %.32q, this build reads %s: %w",
			v, IRVersion, ErrVersionIncompatible)
	}
	return nil
}

// kinded is the wire shape every TypeDef shares: the adjacent "kind" tag first,
// then the concrete kind's fields beside it. T is always a method-less alias of
// a kind, so encoding Body cannot re-enter that kind's own methods.
type kinded[T any] struct {
	Kind TypeKind `json:"kind"`
	Body *T       `json:",embed"`
}

// marshalKinded writes body, an alias of the public kind P, as a kind-tagged
// object.
func marshalKinded[P, T any](enc *jsontext.Encoder, k TypeKind, body *T) error {
	err := json.MarshalEncode(enc, kinded[T]{Kind: k, Body: body})
	return named[P](err, reflect.TypeFor[kinded[T]]())
}

// unmarshalKinded reads a kind-tagged object into body, an alias of the public
// kind P, refusing a tag that names any other kind.
func unmarshalKinded[P, T any](dec *jsontext.Decoder, k TypeKind, body *T) error {
	w := kinded[T]{Body: body}
	if err := json.UnmarshalDecode(dec, &w); err != nil {
		return named[P](err, reflect.TypeFor[kinded[T]]())
	}
	if w.Kind != k {
		return fmt.Errorf("ir: %s carries the kind tag %q", k, w.Kind)
	}
	return nil
}

// named rewrites an error raised on internal, a type this file codes through,
// to name the public type P, so a caller reads ir.Model rather than a type it
// has no way to spell.
func named[P any](err error, internal reflect.Type) error {
	if serr, ok := errors.AsType[*json.SemanticError](err); ok && serr.GoType == internal {
		serr.GoType = reflect.TypeFor[P]()
	}
	return err
}

// MarshalJSONTo encodes the Primitive with an adjacent "kind" tag.
func (p *Primitive) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Primitive
	return marshalKinded[Primitive](enc, KindPrimitive, (*alias)(p))
}

// UnmarshalJSONFrom decodes a kind-tagged Primitive.
func (p *Primitive) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Primitive
	return unmarshalKinded[Primitive](dec, KindPrimitive, (*alias)(p))
}

// MarshalJSONTo encodes the Scalar with an adjacent "kind" tag.
func (s *Scalar) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Scalar
	return marshalKinded[Scalar](enc, KindScalar, (*alias)(s))
}

// UnmarshalJSONFrom decodes a kind-tagged Scalar.
func (s *Scalar) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Scalar
	return unmarshalKinded[Scalar](dec, KindScalar, (*alias)(s))
}

// MarshalJSONTo encodes the Model with an adjacent "kind" tag.
func (m *Model) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Model
	return marshalKinded[Model](enc, KindModel, (*alias)(m))
}

// UnmarshalJSONFrom decodes a kind-tagged Model.
func (m *Model) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Model
	return unmarshalKinded[Model](dec, KindModel, (*alias)(m))
}

// MarshalJSONTo encodes the Union with an adjacent "kind" tag.
func (u *Union) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Union
	return marshalKinded[Union](enc, KindUnion, (*alias)(u))
}

// UnmarshalJSONFrom decodes a kind-tagged Union.
func (u *Union) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Union
	return unmarshalKinded[Union](dec, KindUnion, (*alias)(u))
}

// MarshalJSONTo encodes the Enum with an adjacent "kind" tag.
func (e *Enum) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Enum
	return marshalKinded[Enum](enc, KindEnum, (*alias)(e))
}

// UnmarshalJSONFrom decodes a kind-tagged Enum.
func (e *Enum) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Enum
	return unmarshalKinded[Enum](dec, KindEnum, (*alias)(e))
}

// MarshalJSONTo encodes the List with an adjacent "kind" tag.
func (l *List) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias List
	return marshalKinded[List](enc, KindList, (*alias)(l))
}

// UnmarshalJSONFrom decodes a kind-tagged List.
func (l *List) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias List
	return unmarshalKinded[List](dec, KindList, (*alias)(l))
}

// MarshalJSONTo encodes the MapT with an adjacent "kind" tag.
func (m *MapT) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias MapT
	return marshalKinded[MapT](enc, KindMap, (*alias)(m))
}

// UnmarshalJSONFrom decodes a kind-tagged MapT.
func (m *MapT) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias MapT
	return unmarshalKinded[MapT](dec, KindMap, (*alias)(m))
}

// MarshalJSONTo encodes the Tuple with an adjacent "kind" tag.
func (t *Tuple) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Tuple
	return marshalKinded[Tuple](enc, KindTuple, (*alias)(t))
}

// UnmarshalJSONFrom decodes a kind-tagged Tuple.
func (t *Tuple) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Tuple
	return unmarshalKinded[Tuple](dec, KindTuple, (*alias)(t))
}

// MarshalJSONTo encodes the Literal with an adjacent "kind" tag.
func (l *Literal) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Literal
	return marshalKinded[Literal](enc, KindLiteral, (*alias)(l))
}

// UnmarshalJSONFrom decodes a kind-tagged Literal.
func (l *Literal) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Literal
	return unmarshalKinded[Literal](dec, KindLiteral, (*alias)(l))
}

// MarshalJSONTo encodes the External with an adjacent "kind" tag.
func (e *External) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias External
	return marshalKinded[External](enc, KindExternal, (*alias)(e))
}

// UnmarshalJSONFrom decodes a kind-tagged External.
func (e *External) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias External
	return unmarshalKinded[External](dec, KindExternal, (*alias)(e))
}

// MarshalJSONTo encodes the Any with an adjacent "kind" tag.
func (a *Any) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Any
	return marshalKinded[Any](enc, KindAny, (*alias)(a))
}

// UnmarshalJSONFrom decodes a kind-tagged Any.
func (a *Any) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias Any
	return unmarshalKinded[Any](dec, KindAny, (*alias)(a))
}

// UnmarshalJSONFrom decodes a kind-tagged TypeDef per entry, dispatching through
// the same registry the completeness test walks. Each entry is decoded under the
// options of the decoder that reached it, which is what holds a TypeDef's body
// to the same rules as the rest of the document.
//
// Entries are decoded in ID order, so a registry with several bad entries names
// the same one on every run.
//
// A JSON null is a no-op (#46): the registry is left as it was. Where that
// differs from a plain map, a null decoded over a populated registry, the
// Unmarshaler convention time.Time follows was chosen over the map's.
func (r *TypeRegistry) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var rawByID map[TypeID]jsontext.Value
	if err := json.UnmarshalDecode(dec, &rawByID); err != nil {
		return fmt.Errorf("ir: type registry: %w", err)
	}
	if rawByID == nil {
		return nil
	}
	out := make(TypeRegistry, len(rawByID))
	for _, id := range slices.Sorted(maps.Keys(rawByID)) {
		td, err := decodeTypeDef(id, rawByID[id], dec.Options())
		if err != nil {
			return err
		}
		out[id] = td
	}
	*r = out
	return nil
}

// decodeTypeDef decodes one kind-tagged entry into the concrete kind its tag
// names.
func decodeTypeDef(id TypeID, raw jsontext.Value, opts json.Options) (TypeDef, error) {
	k, found, err := stringMember(raw, "kind")
	if err != nil {
		return nil, fmt.Errorf("ir: type %s: reading kind tag: %w", id, err)
	}
	if !found {
		return nil, fmt.Errorf("ir: type %s: reading kind tag: no \"kind\" member", id)
	}
	td, ok := NewTypeDef(TypeKind(k))
	if !ok {
		return nil, fmt.Errorf("ir: type %s: unknown kind %q", id, k)
	}
	if err := json.Unmarshal(raw, td, opts); err != nil {
		return nil, fmt.Errorf("ir: type %s (%s): %w", id, k, err)
	}
	return td, nil
}

// stringMember reads the string member name of the JSON object raw without
// decoding the rest of it, reporting whether the object has one. The encoder
// writes both members this is asked for first, so the scan normally stops at
// the first member; otherwise it is bounded by the object's own members.
func stringMember(raw jsontext.Value, name string) (string, bool, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.ReadToken()
	if err != nil {
		return "", false, err
	}
	if tok.Kind() != '{' {
		return "", false, fmt.Errorf("want a JSON object, got %s", tok.Kind())
	}
	for dec.PeekKind() == '"' {
		key, err := dec.ReadToken()
		if err != nil {
			return "", false, err
		}
		if key.String() != name {
			if err := dec.SkipValue(); err != nil {
				return "", false, err
			}
			continue
		}
		v, err := dec.ReadToken()
		if err != nil {
			return "", false, err
		}
		if v.Kind() != '"' {
			return "", false, fmt.Errorf("%q is a JSON %s, not a string", name, v.Kind())
		}
		return v.String(), true, nil
	}
	// The scan stops at the object's close or at whatever PeekKind could not
	// read; reading that token reports an object that never ended instead of
	// reading it as one without the member.
	if _, err := dec.ReadToken(); err != nil {
		return "", false, err
	}
	return "", false, nil
}
