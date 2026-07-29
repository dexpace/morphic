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

// typeIDType is the reflect.Type of ir.TypeID. The walk recognizes a reference
// by its declared Go type, never by field name: every TypeID-typed field in the
// IR is a reference into Document.Types — a node's own ID included, which
// resolves against its own registry entry — so no field has to be listed here
// and none can be forgotten.
var typeIDType = reflect.TypeOf(ir.TypeID(""))

// refVisitor receives every TypeID reached by the walk, along with a
// human-readable location used for diagnostic provenance.
type refVisitor func(target ir.TypeID, where string)

// typeRefSite is one discovered TypeID reference and where it was found.
type typeRefSite struct {
	target ir.TypeID
	where  string
}

// refWalk carries the mutable state of one bounded, cycle-guarded traversal.
type refWalk struct {
	visit     refVisitor
	seen      map[uintptr]bool
	truncated bool
}

// collectTypeIDs returns every TypeID reachable from root, sorted by location so
// Go's randomized map iteration never reaches the diagnostics (invariant 7), and
// reports whether the depth cap truncated the walk.
func collectTypeIDs(root any, path string) ([]typeRefSite, bool) {
	var sites []typeRefSite
	truncated := walkTypeIDs(root, path, func(target ir.TypeID, where string) {
		sites = append(sites, typeRefSite{target: target, where: where})
	})
	// Every site has its own field path, so location alone totally orders them.
	slices.SortFunc(sites, func(a, b typeRefSite) int {
		return strings.Compare(a.where, b.where)
	})
	return sites, truncated
}

// walkTypeIDs visits every non-empty ir.TypeID reachable from root, reporting
// whether the depth cap truncated the walk. Deriving the sites from the value
// graph instead of naming fields is what makes the walk complete: a ref-bearing
// field added to the IR is covered the moment it exists, which a hand-written
// enumeration cannot promise.
func walkTypeIDs(root any, path string, visit refVisitor) bool {
	w := refWalk{visit: visit, seen: map[uintptr]bool{}}
	w.walk(reflect.ValueOf(root), path, 0)
	return w.truncated
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
		if v.Type() == typeIDType && v.String() != "" {
			w.visit(ir.TypeID(v.String()), path)
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
		// Numbers, bools, funcs, channels and the invalid Value hold no TypeID.
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
// in a document, while a byte element can hold no TypeID.
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
