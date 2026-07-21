package protobuf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/ir"
)

// maxOptionDepth bounds recursion through nested custom-option message values
// (styleguide bounded-recursion rule); deeply nested options degrade gracefully.
const maxOptionDepth = 32

// deprecationOf returns a Deprecation when the descriptor carries the standard
// `deprecated` option, else nil.
func deprecationOf(d protoreflect.Descriptor) *ir.Deprecation {
	if optionBool(d, "deprecated") {
		return &ir.Deprecation{}
	}
	return nil
}

// optionBool reads a boolean standard option by name, returning false when the
// option field is unset. The parser always materializes an options message, so
// the value is read from it directly.
func optionBool(d protoreflect.Descriptor, name protoreflect.Name) bool {
	m := optionsMessage(d)
	fd := m.Descriptor().Fields().ByName(name)
	if fd == nil {
		return false
	}
	return m.Get(fd).Bool()
}

// optionsMessage returns the descriptor's options as a reflective message. The
// parser materializes an options message on every descriptor (empty when nothing
// is set), so the result is always a valid message.
func optionsMessage(d protoreflect.Descriptor) protoreflect.Message {
	return d.Options().ProtoReflect()
}

// customOptions renders a descriptor's custom (extension) options into
// Extensions, keyed by the option's fully-qualified name and namespaced under
// "protobuf:option:". Standard options the compiler models elsewhere are
// excluded; ordering is deterministic.
func (l *lowerer) customOptions(d protoreflect.Descriptor) ir.Extensions {
	m := optionsMessage(d)
	type entry struct {
		key string
		raw ir.RawValue
	}
	var entries []entry
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !fd.IsExtension() {
			return true // standard options are modeled by dedicated fields
		}
		if raw, ok := renderValue(fd, v, 0); ok {
			entries = append(entries, entry{"protobuf:option:" + string(fd.FullName()), raw})
		}
		return true
	})
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	ext := make(ir.Extensions, len(entries))
	for _, e := range entries {
		ext[e.key] = e.raw
	}
	return ext
}

// renderValue renders one option field value (scalar, message, list, or map)
// into deterministic JSON.
func renderValue(fd protoreflect.FieldDescriptor, v protoreflect.Value, depth int) (ir.RawValue, bool) {
	if depth > maxOptionDepth {
		return nil, false
	}
	switch {
	case fd.IsList():
		return renderList(fd, v.List(), depth)
	case fd.IsMap():
		return renderMap(fd, v.Map(), depth)
	default:
		return renderScalar(fd, v, depth)
	}
}

// renderList renders a repeated option value into a JSON array.
func renderList(fd protoreflect.FieldDescriptor, list protoreflect.List, depth int) (ir.RawValue, bool) {
	parts := make([]json.RawMessage, 0, list.Len())
	for i := range list.Len() {
		if raw, ok := renderScalar(fd, list.Get(i), depth); ok {
			parts = append(parts, raw)
		}
	}
	b, _ := json.Marshal(parts) // a slice of RawMessage always marshals
	return ir.RawValue(b), true
}

// renderMap renders a map option value into a JSON object with key-sorted
// entries for determinism.
func renderMap(fd protoreflect.FieldDescriptor, mp protoreflect.Map, depth int) (ir.RawValue, bool) {
	type entry struct {
		key string
		raw json.RawMessage
	}
	var entries []entry
	valField := fd.MapValue()
	mp.Range(func(mk protoreflect.MapKey, v protoreflect.Value) bool {
		if raw, ok := renderScalar(valField, v, depth); ok {
			entries = append(entries, entry{mk.String(), raw})
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	return objectRaw(len(entries), func(i int) (string, json.RawMessage) {
		return entries[i].key, entries[i].raw
	}), true
}

// renderScalar renders a singular option value into JSON: nested messages
// recurse, enums render by member name, bytes as base64, and other scalars as
// their JSON form.
func renderScalar(fd protoreflect.FieldDescriptor, v protoreflect.Value, depth int) (ir.RawValue, bool) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return renderMessage(v.Message(), depth+1)
	case protoreflect.EnumKind:
		return jsonRaw(enumMemberName(fd.Enum(), v.Enum()))
	case protoreflect.BytesKind:
		return jsonRaw(base64.StdEncoding.EncodeToString(v.Bytes()))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return jsonRaw(v.Uint())
	default:
		return jsonRaw(v.Interface())
	}
}

// enumMemberName resolves an enum number to its declared member name, falling
// back to the raw number when the value is undeclared.
func enumMemberName(ed protoreflect.EnumDescriptor, n protoreflect.EnumNumber) any {
	if ev := ed.Values().ByNumber(n); ev != nil {
		return string(ev.Name())
	}
	return int64(n)
}

// renderMessage renders a message option value into a JSON object with its set
// fields ordered by field number for determinism.
func renderMessage(m protoreflect.Message, depth int) (ir.RawValue, bool) {
	if depth > maxOptionDepth {
		return nil, false
	}
	type entry struct {
		num int32
		key string
		raw json.RawMessage
	}
	var entries []entry
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if raw, ok := renderValue(fd, v, depth); ok {
			entries = append(entries, entry{int32(fd.Number()), string(fd.Name()), raw})
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].num < entries[j].num })
	return objectRaw(len(entries), func(i int) (string, json.RawMessage) {
		return entries[i].key, entries[i].raw
	}), true
}

// objectRaw assembles a JSON object from n key/value pairs supplied by at.
func objectRaw(n int, at func(int) (string, json.RawMessage)) ir.RawValue {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		key, raw := at(i)
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(raw)
	}
	b.WriteByte('}')
	return ir.RawValue(b.Bytes())
}

// jsonRaw marshals a Go value into a RawValue.
func jsonRaw(v any) (ir.RawValue, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return ir.RawValue(b), true
}

// halfOpenRanges converts protobuf field ranges ([start, end)) into inclusive
// IR wire-ID ranges.
func halfOpenRanges(ranges protoreflect.FieldRanges) []ir.WireIDRange {
	if ranges.Len() == 0 {
		return nil
	}
	out := make([]ir.WireIDRange, 0, ranges.Len())
	for i := range ranges.Len() {
		r := ranges.Get(i)
		out = append(out, ir.WireIDRange{From: int(r[0]), To: int(r[1]) - 1})
	}
	return out
}

// inclusiveEnumRanges converts protobuf enum ranges ([start, end]) into
// inclusive IR wire-ID ranges.
func inclusiveEnumRanges(ranges protoreflect.EnumRanges) []ir.WireIDRange {
	if ranges.Len() == 0 {
		return nil
	}
	out := make([]ir.WireIDRange, 0, ranges.Len())
	for i := range ranges.Len() {
		r := ranges.Get(i)
		out = append(out, ir.WireIDRange{From: int(r[0]), To: int(r[1])})
	}
	return out
}

// nameList copies a protobuf reserved-name list into a plain string slice.
func nameList(names protoreflect.Names) []string {
	if names.Len() == 0 {
		return nil
	}
	out := make([]string, 0, names.Len())
	for i := range names.Len() {
		out = append(out, string(names.Get(i)))
	}
	return out
}

// reservedRaw renders reserved ranges and names into deterministic JSON, or nil
// when both are empty.
func reservedRaw(ranges []ir.WireIDRange, names []string) ir.RawValue {
	if len(ranges) == 0 && len(names) == 0 {
		return nil
	}
	payload := struct {
		Ranges []ir.WireIDRange `json:"ranges,omitempty"`
		Names  []string         `json:"names,omitempty"`
	}{Ranges: ranges, Names: names}
	b, _ := json.Marshal(payload) // ranges and names always marshal
	return ir.RawValue(b)
}

// mergeRaw sets key to raw in ext, allocating the map on first use.
func mergeRaw(ext ir.Extensions, key string, raw ir.RawValue) ir.Extensions {
	if ext == nil {
		ext = ir.Extensions{}
	}
	ext[key] = raw
	return ext
}
