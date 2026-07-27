package irverify

import (
	"fmt"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// maxWalkDepth bounds the reflection traversal (bounded-recursion rule). Value
// trees (defaults, examples) embed []Value/[]Field by value and are the deepest
// structures the walk reaches; each nesting level costs a few reflection
// descents on top of a fixed prefix from the document root. Compilers bound
// value/example nesting (the OpenAPI compiler caps it at 128), so this limit is
// set well above the deepest reflection path a validly-bounded document can
// produce — several times prefix + maxValueDepth × per-level cost — so a
// walk-truncated violation signals a genuinely pathological document, never a
// valid deeply-nested default. Hitting it is never silent: walkValues reports
// truncation and checkReferentialIntegrity surfaces it as ir/walk-truncated.
const maxWalkDepth = 4096

// refSite is one discovered ID reference and the reference class (refKind) it
// must resolve in.
type refSite struct {
	id   string
	kind refKind
	path string
}

// resolves reports whether s.id exists in the registry s.kind names. The
// has == nil guard — rather than calling s.kind.has directly — keeps Verify a
// report-only oracle that never crashes on a malformed document, even if a
// refSite is ever built with a kind this package does not recognize.
func (s refSite) resolves(doc *ir.Document) bool {
	if s.kind.has == nil {
		return false
	}
	return s.kind.has(doc, s.id)
}

// refKind is one class of typed-ID reference this checker resolves: its
// registry label (used in violation paths and messages), the diagnostic-code
// singular for a dangling reference, and how to test whether an id resolves
// in that registry. refKindByType, keyed by reflect.Type, is the single
// source of truth for what a "reference" is to this checker — a reference
// class can no longer be silently dropped from the oracle by updating one of
// several parallel lists and missing another.
//
// PropID and ServiceID are intentionally absent: neither lives in a
// document-level flat registry, so they are out of scope for Phase 1.
type refKind struct {
	registry string
	singular string
	has      func(doc *ir.Document, id string) bool
}

// refKindByType maps a typed-ID reflect.Type to its refKind. Literals below
// are keyed, not positional: registry and singular are both plain strings,
// and a positional row could silently swap them, emitting
// "ir/dangling-types-ref" instead of "ir/dangling-type-ref".
var refKindByType = map[reflect.Type]refKind{
	reflect.TypeOf(ir.TypeID("")): {registry: "types", singular: "type", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Types[ir.TypeID(id)]
		return ok
	}},
	reflect.TypeOf(ir.AuthID("")): {registry: "auth", singular: "auth", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Auth[ir.AuthID(id)]
		return ok
	}},
	reflect.TypeOf(ir.ChannelID("")): {registry: "channels", singular: "channel", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Channels[ir.ChannelID(id)]
		return ok
	}},
	reflect.TypeOf(ir.MessageID("")): {registry: "messages", singular: "message", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Messages[ir.MessageID(id)]
		return ok
	}},
}

// collectRefs walks doc and returns every non-empty typed-ID reference plus
// whether the bounded walk was truncated. It inspects struct fields, slice/array
// elements, and both map keys and values. Most map keys are an entry's own ID (a
// definition that resolves trivially), but some — Service.Renames's
// map[TypeID]Naming keys — are genuine references into a registry that must
// resolve, so keys are collected too; a node's own ID also resolves trivially,
// so collecting it is harmless.
func collectRefs(doc *ir.Document) ([]refSite, bool) {
	var sites []refSite
	truncated := walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.String {
			return true
		}
		if k, ok := refKindByType[v.Type()]; ok && v.String() != "" {
			sites = append(sites, refSite{id: v.String(), kind: k, path: path})
		}
		return true
	})
	return sites, truncated
}

// checkReferentialIntegrity asserts every discovered reference resolves in its
// registry, emitting one dangling-*-ref Violation per unresolved reference.
func checkReferentialIntegrity(doc *ir.Document) []Violation {
	var vs []Violation
	sites, truncated := collectRefs(doc)
	if truncated {
		vs = append(vs, Violation{
			Code:    "ir/walk-truncated",
			Message: "document nests deeper than the bounded verifier walk; some references and names went unchecked",
			Path:    "doc",
		})
	}
	for _, s := range sites {
		if s.resolves(doc) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/dangling-" + s.kind.singular + "-ref",
			Message: "reference " + s.id + " does not resolve in " + s.kind.registry,
			Path:    s.path,
		})
	}
	return vs
}

// walkValues performs a bounded, cycle-guarded reflection traversal of root,
// invoking visit for every value it reaches. When visit returns false the walk
// does not descend into that value's children; when it returns true the walk
// continues into struct fields, slice/array elements, and both map KEYS and
// VALUES. Keys are walked because some IR maps key by a reference rather than by
// the entry's own identity (Service.Renames is map[TypeID]Naming); only the flat
// Document registries key by a definition, and those keys resolve trivially.
//
// walkValues returns true when the depth cap was reached and at least one real
// child was skipped, so callers can surface a too-deep document rather than
// silently under-checking it.
func walkValues(root any, visit func(v reflect.Value, path string) bool) bool {
	seen := map[uintptr]bool{}
	truncated := false
	var walk func(v reflect.Value, path string, depth int)
	descend := func(child reflect.Value, path string, depth int) {
		if depth > maxWalkDepth {
			truncated = true
			return
		}
		walk(child, path, depth)
	}
	walk = func(v reflect.Value, path string, depth int) {
		if !v.IsValid() || !visit(v, path) {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			p := v.Pointer()
			if seen[p] {
				return
			}
			seen[p] = true
			descend(v.Elem(), path, depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				descend(v.Elem(), path, depth+1)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				descend(v.Field(i), path+"."+v.Type().Field(i).Name, depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				descend(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		case reflect.Map:
			iter := v.MapRange()
			for iter.Next() {
				k := iter.Key()
				descend(k, fmt.Sprintf("%s[%v].key", path, k), depth+1)
				descend(iter.Value(), fmt.Sprintf("%s[%v]", path, k), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(root), "doc", 0)
	return truncated
}
