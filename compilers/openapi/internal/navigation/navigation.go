// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package navigation

import (
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/marshaller"
	yaml "go.yaml.in/yaml/v3"
)

// maxNavigableHops bounds how many objects ReadingOf reads through to the one
// each stands for (jsonpointer.NavigableNoder). The library follows them
// unbounded. Past the bound ReadingOf places nothing, and the library reads the
// token itself, so the bound costs only the shortcut.
const maxNavigableHops = 16

// model is what the library navigates by its core's keys: jsonpointer's own
// unexported model interface.
type model interface {
	GetCoreAny() any
	SetCoreAny(core any)
}

// sequencedMap is the library's internal interfaces.SequencedMapInterface. The
// library asks a map a model embeds by value for a key only when the map's
// address has these methods.
type sequencedMap interface {
	Init()
	IsInitialized() bool
	SetUntyped(key, value any) error
	AllUntyped() iter.Seq2[any, any]
	GetKeyType() reflect.Type
	GetValueType() reflect.Type
	Len() int
	GetAny(key any) (any, bool)
	SetAny(key, value any)
	DeleteAny(key any)
	KeysAny() iter.Seq[any]
}

// Tokens returns the tokens of pointer as the library's walk compares them,
// each decoded, and none for "/", which the library reads as the root where
// jsontext reads one empty token. It reports false for a pointer the library
// refuses before reading anything.
func Tokens(pointer jsontext.Pointer) ([]string, bool) {
	if jsonpointer.JSONPointer(pointer).Validate() != nil {
		return nil, false
	}
	if pointer == "/" {
		return nil, true
	}
	return slices.Collect(pointer.Tokens()), true
}

// Step returns what node holds under the one token, as the library's walk
// reads it on to the next, and false where it holds nothing. Only the empty
// token is read below an envelope keyed by the empty string, since the library
// reads the pointer "/" as the root; on raw YAML the empty token still answers
// the node itself. A model held by value, such as an operation's responses,
// comes back as a pointer to a copy: the walk reads on from its address, where
// its methods navigate it (GitHub #779).
func Step(node any, token string) (any, bool) {
	next, err := step(node, token)
	return next, err == nil
}

// step is Step, with the library's error where node holds nothing.
func step(node any, token string) (any, error) {
	source, pointer := node, "/"+jsonpointer.EscapeString(token)
	if token == "" {
		source, pointer = map[string]any{"": node}, "//"
	}
	next, err := jsonpointer.GetTarget(source, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if v := reflect.ValueOf(next); v.Kind() == reflect.Struct && reflect.PointerTo(v.Type()).Implements(reflect.TypeFor[model]()) {
		p := reflect.New(v.Type())
		p.Elem().Set(v)
		next = p.Interface()
	}
	return next, err
}

// Reading is how the library's read of one token from a node goes on.
type Reading int

const (
	// Answering is a read a field or an entry answers, or that fails there.
	Answering Reading = iota
	// Leaving is a read of the token, and the rest after it, in raw YAML.
	Leaving
	// Retried is a read of an index token as a key with the rest below it,
	// and as an index once that fails, so what answers it turns on the rest.
	Retried
)

// ReadingOf returns how the library's read of token from node goes on, and the
// raw YAML it is read in where it leaves the model, with the tokens after it:
// node itself when it is raw YAML, else the mapping an object was built from
// where none of its fields or embedded maps answers token. It follows the
// library's dispatch for one token (getStructTarget, navigateModel), asking
// each map the library asks through the map's own method.
func ReadingOf(node any, token string) (Reading, *yaml.Node) {
	for range maxNavigableHops {
		switch n := node.(type) {
		case *yaml.Node:
			if n == nil {
				return Answering, nil // the library fails on no node
			}
			return Leaving, n
		case yaml.Node:
			return Leaving, &n
		}
		v := reflect.ValueOf(node)
		if reflect.Indirect(v).Kind() != reflect.Struct {
			return Answering, nil // nothing, a nil pointer, a map, a list or a scalar
		}
		noder, ok := node.(jsonpointer.NavigableNoder)
		if !ok {
			return objectReading(v, token)
		}
		stands, err := noder.GetNavigableNode()
		if err != nil {
			return Answering, nil
		}
		node = stands
	}
	return Answering, nil
}

// objectReading is ReadingOf for an object the library reads by its fields: a
// model by its core's keys, after any map it embeds, and another struct by its
// own fields' keys or names, unless it is a map. Where nothing answers token,
// the library reads it in the mapping the object was built from. An index on
// a struct the library also reads by index is retried (Retried).
func objectReading(v reflect.Value, token string) (Reading, *yaml.Node) {
	if m, ok := reflect.TypeAssert[model](v); ok {
		core := reflect.Indirect(reflect.ValueOf(m.GetCoreAny()))
		if core.Kind() != reflect.Struct || embedsAnswer(v, token) || keyed(core.Type(), token, false) {
			return Answering, nil
		}
		return builtFrom(v)
	}
	if _, ok := reflect.TypeAssert[jsonpointer.IndexNavigable](v); ok && IsIndex(token) {
		return Retried, nil
	}
	if _, ok := reflect.TypeAssert[jsonpointer.KeyNavigable](v); ok {
		return Answering, nil // it answers token or fails, never deferring to its fields (TestWalk_AnswersAsTheWholeReadDoes)
	}
	if keyed(reflect.Indirect(v).Type(), token, true) {
		return Answering, nil
	}
	return builtFrom(v)
}

// embedsAnswer reports whether a map the model in v embeds answers key, as the
// library asks each before the model's own fields: one embedded by pointer, or
// by value where the library takes its address.
func embedsAnswer(v reflect.Value, key string) bool {
	s := reflect.Indirect(v)
	for i := range s.NumField() {
		if !s.Type().Field(i).Anonymous {
			continue
		}
		if m, ok := embeddedMap(s.Field(i)); ok {
			if _, err := m.NavigateWithKey(key); err == nil {
				return true
			}
		}
	}
	return false
}

// embeddedMap returns the map an embedded field f is, as navigateModel reads
// one: a pointer that is not nil, or the address of a value whose pointer is a
// sequencedMap.
func embeddedMap(f reflect.Value) (jsonpointer.KeyNavigable, bool) {
	if f.Kind() == reflect.Pointer {
		if f.IsNil() {
			return nil, false
		}
		m, ok := reflect.TypeAssert[jsonpointer.KeyNavigable](f)
		return m, ok
	}
	if !f.CanAddr() || !reflect.PointerTo(f.Type()).Implements(reflect.TypeFor[sequencedMap]()) {
		return nil, false
	}
	m, ok := reflect.TypeAssert[jsonpointer.KeyNavigable](f.Addr())
	return m, ok
}

// keyed reports whether an exported field of the struct type t answers key:
// one whose key tag is key, or, byName, one with no key tag named key. A
// model's core is read by tag alone, so there an untagged field answers the
// empty key, as the library finds it.
func keyed(t reflect.Type, key string, byName bool) bool {
	// Indexed: ranging over t.Fields() costs four allocations a call.
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Tag.Get("key")
		if byName && name == "" {
			name = f.Name
		}
		if name == key {
			return true
		}
	}
	return false
}

// builtFrom returns the mapping the object in v was built from, which the
// library reads a token in that no field answers, or that it fails without one.
func builtFrom(v reflect.Value) (Reading, *yaml.Node) {
	built, ok := reflect.TypeAssert[marshaller.RootNodeAccessor](v)
	if !ok {
		return Answering, nil
	}
	root := built.GetRootNode()
	if root == nil {
		return Answering, nil
	}
	return Leaving, root
}

// IsIndex reports whether the library types token as an index: ASCII digits,
// with no leading zero unless the token is "0". It reads a token alike escaped
// or decoded, since no escape decodes to a digit.
func IsIndex(token string) bool {
	if token == "" || len(token) > 1 && token[0] == '0' {
		return false
	}
	return strings.Trim(token, "0123456789") == ""
}
