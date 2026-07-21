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

// refSite is one discovered ID reference and the registry it must resolve in.
type refSite struct {
	id       string
	registry string // "types" | "auth" | "channels" | "messages"
	path     string
}

var (
	typeIDType    = reflect.TypeOf(ir.TypeID(""))
	authIDType    = reflect.TypeOf(ir.AuthID(""))
	channelIDType = reflect.TypeOf(ir.ChannelID(""))
	messageIDType = reflect.TypeOf(ir.MessageID(""))
)

// registryFor maps a typed-ID reflect.Type to its registry label, or "" if the
// string type is not a reference this checker resolves. PropID and ServiceID are
// intentionally absent: neither lives in a document-level flat registry, so they
// are out of scope for Phase 1.
func registryFor(t reflect.Type) string {
	switch t {
	case typeIDType:
		return "types"
	case authIDType:
		return "auth"
	case channelIDType:
		return "channels"
	case messageIDType:
		return "messages"
	default:
		return ""
	}
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
		if reg := registryFor(v.Type()); reg != "" && v.String() != "" {
			sites = append(sites, refSite{id: v.String(), registry: reg, path: path})
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
		if resolves(doc, s) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/dangling-" + singular(s.registry) + "-ref",
			Message: "reference " + s.id + " does not resolve in " + s.registry,
			Path:    s.path,
		})
	}
	return vs
}

// resolves reports whether s.id exists in the registry s names.
func resolves(doc *ir.Document, s refSite) bool {
	switch s.registry {
	case "types":
		_, ok := doc.Types[ir.TypeID(s.id)]
		return ok
	case "auth":
		_, ok := doc.Auth[ir.AuthID(s.id)]
		return ok
	case "channels":
		_, ok := doc.Channels[ir.ChannelID(s.id)]
		return ok
	case "messages":
		_, ok := doc.Messages[ir.MessageID(s.id)]
		return ok
	default:
		return false
	}
}

// singular converts a registry label to its diagnostic-code singular form.
func singular(registry string) string {
	switch registry {
	case "types":
		return "type"
	case "channels":
		return "channel"
	case "messages":
		return "message"
	default:
		return registry // "auth" is already singular
	}
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
