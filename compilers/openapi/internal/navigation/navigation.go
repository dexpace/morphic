package navigation

import (
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/marshaller"
	yaml "gopkg.in/yaml.v3"
)

// maxNavigableHops bounds how many objects Leaves reads through to the one each
// stands for (jsonpointer.NavigableNoder). The library follows them unbounded;
// none in v1.25.2 stands for another past two hops. Past the bound Leaves
// places nothing, and the library reads the token itself.
const maxNavigableHops = 16

// model is what the library navigates by its core's keys: jsonpointer's own
// unexported model interface (v1.25.2).
type model interface {
	GetCoreAny() any
	SetCoreAny(core any)
}

// sequencedMap is the library's internal interfaces.SequencedMapInterface
// (v1.25.2). The library asks a map a model embeds by value for a key only when
// the map's address has these methods.
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
// each decoded, and none for "/", which it reads as the root. It reports false
// for a pointer the library refuses before reading anything.
func Tokens(pointer jsontext.Pointer) ([]string, bool) {
	if jsonpointer.JSONPointer(pointer).Validate() != nil {
		return nil, false
	}
	if pointer == "/" {
		return nil, true
	}
	tokens := strings.Split(string(pointer[1:]), "/")
	for i, token := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens, true
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

// Walk reads tokens from node as the library's read of the pointer they spell
// walks them, a step at a time, until a step leaves the model. It returns where
// the walk stopped and the tokens left there, which the library reads together
// in that raw YAML: none when the model answered every token. Where a step
// finds nothing, the error is the library's.
func Walk(node any, tokens []string) (any, []string, error) {
	for i, token := range tokens {
		if raw, leaves := Leaves(node, token); leaves {
			return raw, tokens[i:], nil
		}
		next, err := step(node, token)
		if err != nil {
			return nil, nil, err
		}
		node = next
	}
	return node, nil, nil
}

// Leaves returns the raw YAML the library's read of token from node goes on in,
// with the tokens after it read together there, and true: node itself when it
// is raw YAML, else the mapping an object was built from where none of its
// fields or embedded maps answers token. It is false where one does, or where
// the read fails first. It follows the library's dispatch for one token
// (getStructTarget, navigateModel, v1.25.2), asking each map the library asks
// through the map's own method.
func Leaves(node any, token string) (*yaml.Node, bool) {
	for range maxNavigableHops {
		switch n := node.(type) {
		case *yaml.Node:
			return n, n != nil
		case yaml.Node:
			return &n, true
		}
		v := reflect.ValueOf(node)
		if reflect.Indirect(v).Kind() != reflect.Struct {
			return nil, false // nothing, a nil pointer, a map, a list or a scalar
		}
		noder, ok := node.(jsonpointer.NavigableNoder)
		if !ok {
			return objectLeaves(v, token)
		}
		stands, err := noder.GetNavigableNode()
		if err != nil {
			return nil, false
		}
		node = stands
	}
	return nil, false
}

// objectLeaves is Leaves for an object the library reads by its fields: a model
// by its core's keys, after any map it embeds, and another struct by its own
// fields' keys or names, unless it is a map. Where nothing answers token, the
// library reads it in the mapping the object was built from.
func objectLeaves(v reflect.Value, token string) (*yaml.Node, bool) {
	if m, ok := v.Interface().(model); ok {
		core := reflect.Indirect(reflect.ValueOf(m.GetCoreAny()))
		if core.Kind() != reflect.Struct || embedsAnswer(v, token) || keyed(core.Type(), token, false) {
			return nil, false
		}
		return builtFrom(v)
	}
	if _, ok := v.Interface().(jsonpointer.KeyNavigable); ok {
		return nil, false // it answers token or fails; none in v1.25.2 defers to its fields
	}
	if _, ok := v.Interface().(jsonpointer.IndexNavigable); ok && isIndex(token) {
		return nil, false // it is asked only when the raw read fails, which is the library's to find
	}
	if keyed(reflect.Indirect(v).Type(), token, true) {
		return nil, false
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
		m, ok := f.Interface().(jsonpointer.KeyNavigable)
		return m, ok
	}
	if !f.CanAddr() || !reflect.PointerTo(f.Type()).Implements(reflect.TypeFor[sequencedMap]()) {
		return nil, false
	}
	m, ok := f.Addr().Interface().(jsonpointer.KeyNavigable)
	return m, ok
}

// keyed reports whether an exported field of the struct type t answers key:
// one whose key tag is key, or, byName, one with no key tag named key. A
// model's core is read by tag alone, so there an untagged field answers the
// empty key, as the library finds it.
func keyed(t reflect.Type, key string, byName bool) bool {
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
// library reads a token in that no field answers, and false for none.
func builtFrom(v reflect.Value) (*yaml.Node, bool) {
	built, ok := v.Interface().(marshaller.RootNodeAccessor)
	if !ok {
		return nil, false
	}
	root := built.GetRootNode()
	return root, root != nil
}

// isIndex reports whether the library reads token as an index: digits, with
// no leading zero unless it is one.
func isIndex(token string) bool {
	if token == "" || len(token) > 1 && token[0] == '0' {
		return false
	}
	return strings.Trim(token, "0123456789") == ""
}
