package ir

import (
	"reflect"
	"strings"
)

// Registry is one flat, ID-keyed registry a [Document] declares: the entries
// themselves, plus the name a report about them is spelled with.
//
// The zero value declares nothing, which is what a lookup for an ID class the
// document registers no map for yields. [Registry.Has] reports false for it
// rather than indexing an invalid value, so a checker holding a site built
// against another document reports it as unresolved instead of crashing.
type Registry struct {
	// Label is the name of the Document field that declares the registry,
	// lowercased — "types", "channels" — for a report that has to name it.
	Label string

	entries reflect.Value
}

// Has reports whether the registry declares id.
func (r Registry) Has(id string) bool {
	if !r.entries.IsValid() {
		return false
	}
	return r.entries.MapIndex(reflect.ValueOf(id).Convert(r.entries.Type().Key())).IsValid()
}

// Registries maps each ID type to the [Registry] that declares those IDs.
//
// A value's Go type is what makes it a reference: a [ChannelID]-typed field is a
// reference into Document.Channels wherever it sits — a node's own ID included,
// which resolves against its own entry — so no field has to be listed here and
// none can be forgotten.
//
// Type-driven coverage is not total, and what it misses is a category rather
// than a stray field. A reference carried as an integer index into a slice is an
// int like any other, and reflection has nothing to key on; [PropID] names a
// position inside a model rather than an entry in a document-level map. Both
// classes are resolved by hand where they are checked.
type Registries map[reflect.Type]Registry

// DocumentRegistries derives doc's registries from Document's own shape: a field
// that is a map keyed by a named string type is an ID-keyed registry, and its key
// type names the reference class it resolves. Deriving them covers a registry
// added to Document the moment it exists, where a hand-written list would drift.
// Document.Unmodeled is the counterexample: keyed by plain string, it keys on a
// source construct's name rather than an identity, and is no registry.
//
// A nil doc declares nothing, which is the answer a report-only caller wants:
// every reference then resolves against no registry and is reported, rather than
// the call panicking on the way to saying so.
func DocumentRegistries(doc *Document) Registries {
	out := Registries{}
	if doc == nil {
		return out
	}
	for shape, f := range reflect.ValueOf(doc).Elem().Fields() {
		key, isRegistry := registryKeyType(f)
		if !isRegistry {
			continue
		}
		out[key] = Registry{Label: strings.ToLower(shape.Name), entries: f}
	}
	return out
}

// registryKeyType returns the named string type f is keyed by, and whether f is
// an ID-keyed registry at all.
func registryKeyType(f reflect.Value) (reflect.Type, bool) {
	if f.Kind() != reflect.Map {
		return nil, false
	}
	key := f.Type().Key()
	if key.Kind() != reflect.String || key.PkgPath() == "" {
		return nil, false
	}
	return key, true
}

// RefNoun names the reference class an ID type identifies: its type name minus
// the ID suffix, lowercased ("ChannelID" → "channel"). Both of Morphic's
// checkers spell a dangling-reference code with it, so one defect reads under
// one code whichever of them reports it.
func RefNoun(idType reflect.Type) string {
	return strings.ToLower(strings.TrimSuffix(idType.Name(), "ID"))
}
