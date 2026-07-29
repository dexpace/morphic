package pass

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxRefWalkDepth bounds the reflection traversal of a document (the
// bounded-recursion rule). Value trees — defaults, examples — nest deepest, and
// compilers cap their nesting far below this (the OpenAPI compiler at 128), so
// reaching the cap signals a pathological document rather than legitimate
// nesting. The walk reports truncation instead of under-checking in silence.
const maxRefWalkDepth = 4096

// typeIDType is the reflect.Type of ir.TypeID, the one reference class the
// reachability analysis in validate.go needs without a document to resolve
// against.
var typeIDType = reflect.TypeOf(ir.TypeID(""))

// registries maps each ID type to the Document registry that declares those IDs.
// The walk recognizes a reference by its declared Go type, never by field name:
// an ir.ChannelID-typed field is a reference into Document.Channels wherever it
// sits — a node's own ID included, which resolves against its own registry entry
// — so no field has to be listed here and none can be forgotten.
//
// Type-driven coverage is not total, and what it misses is a category rather
// than a stray field: a reference carried as an integer index into a slice is an
// int like any other, and reflection has nothing to key on. The IR has three —
// Service.Servers and Channel.Servers into Document.Servers, and
// HTTPBinding.SuccessStatus's keys into Operation.Responses, both enumerated by
// hand in checkServerIndices and checkResponseIndices; and Provenance.Source into
// Document.Sources, which irverify checks instead. Provenance is left to irverify
// because a stale source index is a compiler bug rather than a spec problem, and
// because this pass stamps Provenance{Source: -1} on its own diagnostics, which
// the engine folds into Document.Diagnostics — a document-wide check here would
// report its own previous output. A new integer-index reference has to be added
// to those checks by hand too, because nothing here will find it; irverify's
// integerFields guard is what stops one being added unnoticed.
type registries map[reflect.Type]reflect.Value

// documentRegistries derives doc's registries from Document's own shape: a field
// that is a map keyed by a named string type is an ID-keyed registry, and its key
// type names the reference class it resolves. Deriving them covers a registry
// added to Document the moment it exists, where a hand-written list would drift.
// Document.Preserved is the counterexample: keyed by plain string, it keys on a
// source construct's name rather than an identity, and is no registry.
func documentRegistries(doc *ir.Document) registries {
	out := registries{}
	fields := reflect.ValueOf(doc).Elem()
	for i := range fields.NumField() {
		f := fields.Field(i)
		if f.Kind() != reflect.Map {
			continue
		}
		key := f.Type().Key()
		if key.Kind() == reflect.String && key.PkgPath() != "" {
			out[key] = f
		}
	}
	return out
}

// isRef reports whether values of Go type t are references this document can
// resolve.
func (r registries) isRef(t reflect.Type) bool {
	_, ok := r[t]
	return ok
}

// resolves reports whether the registry for s.idType declares s.id. The
// unknown-class guard — rather than indexing r directly — keeps the pass
// report-only: collectRefs only builds sites for classes isRef accepted, so an
// unresolvable class means the caller mixed sites from another document, and
// reporting that as dangling beats panicking on a zero reflect.Value.
func (r registries) resolves(s refSite) bool {
	entries, ok := r[s.idType]
	if !ok {
		return false
	}
	return entries.MapIndex(reflect.ValueOf(s.id).Convert(s.idType)).IsValid()
}

// refNoun names the reference class an ID type identifies: its type name minus
// the ID suffix, lowercased ("ChannelID" → "channel"). The same noun spells
// irverify's code for the identical defect, so both checkers report a dangling
// reference under one code rather than two.
func refNoun(idType reflect.Type) string {
	return strings.ToLower(strings.TrimSuffix(idType.Name(), "ID"))
}

// refSite is one discovered ID reference: the class that made it a reference,
// its value, and a human-readable location used for diagnostic provenance.
type refSite struct {
	idType reflect.Type
	id     string
	where  string
}

// refWalk carries the mutable state of one bounded, cycle-guarded traversal.
type refWalk struct {
	isRef     func(reflect.Type) bool
	sites     []refSite
	seen      map[uintptr]bool
	truncated bool
}

// collectRefs returns every reference reachable from root whose Go type isRef
// accepts, sorted by location so Go's randomized map iteration never reaches the
// diagnostics (invariant 7), and reports whether the depth cap truncated the
// walk.
//
// Deriving the sites from the value graph instead of naming fields is what makes
// the walk complete: a ref-bearing field added to the IR is covered the moment it
// exists, which a hand-written enumeration cannot promise.
func collectRefs(root any, path string, isRef func(reflect.Type) bool) ([]refSite, bool) {
	w := refWalk{isRef: isRef, seen: map[uintptr]bool{}}
	w.walk(reflect.ValueOf(root), path, 0)
	// Every site has its own field path, so location alone totally orders them.
	slices.SortFunc(w.sites, func(a, b refSite) int {
		return strings.Compare(a.where, b.where)
	})
	return w.sites, w.truncated
}

// collectTypeIDs returns every ir.TypeID reachable from root. Type reachability
// follows one class, so it names that class rather than resolving against a
// document it is not handed.
func collectTypeIDs(root any, path string) ([]refSite, bool) {
	return collectRefs(root, path, func(t reflect.Type) bool { return t == typeIDType })
}

// descend walks child one level deeper, marking the walk truncated at the cap.
func (w *refWalk) descend(child reflect.Value, path string, depth int) {
	if depth > maxRefWalkDepth {
		w.truncated = true
		return
	}
	w.walk(child, path, depth)
}

// walk dispatches on v's kind. The invalid zero Value — a nil root, a nil
// interface's dynamic value — falls to the default arm, so nothing here calls
// Type() on it.
func (w *refWalk) walk(v reflect.Value, path string, depth int) {
	switch v.Kind() {
	case reflect.String:
		if v.String() != "" && w.isRef(v.Type()) {
			w.sites = append(w.sites, refSite{idType: v.Type(), id: v.String(), where: path})
		}
	case reflect.Pointer:
		w.walkPointer(v, path, depth)
	case reflect.Interface:
		if !v.IsNil() {
			w.descend(v.Elem(), path, depth+1)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			w.descend(v.Field(i), path+"/"+v.Type().Field(i).Name, depth+1)
		}
	case reflect.Slice, reflect.Array:
		w.walkSequence(v, path, depth)
	case reflect.Map:
		w.walkMap(v, path, depth)
	default:
		// Numbers, bools, funcs, channels and the invalid Value hold no typed ID.
	}
}

// walkPointer descends through a non-nil pointer once. The seen set terminates
// cyclic type graphs — normal input for schema languages, not an edge case — and
// means a target reached through a shared pointer is reported once rather than
// once per reference to it.
func (w *refWalk) walkPointer(v reflect.Value, path string, depth int) {
	if v.IsNil() {
		return
	}
	p := v.Pointer()
	if w.seen[p] {
		return
	}
	w.seen[p] = true
	w.descend(v.Elem(), path, depth+1)
}

// walkSequence descends into slice and array elements, skipping byte sequences:
// Preserved and RawConfig payloads are json.RawMessage and are the largest thing
// in a document, while a byte element can hold no typed ID.
func (w *refWalk) walkSequence(v reflect.Value, path string, depth int) {
	if v.Type().Elem().Kind() == reflect.Uint8 {
		return
	}
	for i := range v.Len() {
		w.descend(v.Index(i), fmt.Sprintf("%s/%d", path, i), depth+1)
	}
}

// walkMap descends into keys as well as values: most keys are an entry's own ID,
// but Service.Renames is map[TypeID]Naming, where the key is the reference and
// the value is not.
func (w *refWalk) walkMap(v reflect.Value, path string, depth int) {
	iter := v.MapRange()
	for iter.Next() {
		k := iter.Key()
		w.descend(k, fmt.Sprintf("%s[%v]/key", path, k), depth+1)
		w.descend(iter.Value(), fmt.Sprintf("%s[%v]", path, k), depth+1)
	}
}
