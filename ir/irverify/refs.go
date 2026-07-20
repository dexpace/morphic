package irverify

import (
	"fmt"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// maxWalkDepth bounds the reflection traversal (bounded-recursion rule). The IR
// nests shallowly and references other nodes by ID string rather than by
// pointer, so this cap is a defensive backstop, not a functional limit.
const maxWalkDepth = 256

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

// collectRefs walks doc and returns every non-empty typed-ID reference. It
// visits struct fields, slice/array elements and map VALUES only — map keys and
// a node's own ID are definitions, not references (a node's own ID trivially
// resolves in its registry, so collecting it is harmless).
func collectRefs(doc *ir.Document) []refSite {
	var sites []refSite
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.String {
			return true
		}
		if reg := registryFor(v.Type()); reg != "" && v.String() != "" {
			sites = append(sites, refSite{id: v.String(), registry: reg, path: path})
		}
		return true
	})
	return sites
}

// checkReferentialIntegrity asserts every discovered reference resolves in its
// registry, emitting one dangling-*-ref Violation per unresolved reference.
func checkReferentialIntegrity(doc *ir.Document) []Violation {
	var vs []Violation
	for _, s := range collectRefs(doc) {
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
// continues into struct fields, slice/array elements, and map VALUES (map keys
// are skipped: they are definitions, not references).
func walkValues(root any, visit func(v reflect.Value, path string) bool) {
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value, path string, depth int)
	walk = func(v reflect.Value, path string, depth int) {
		if depth > maxWalkDepth || !v.IsValid() || !visit(v, path) {
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
			walk(v.Elem(), path, depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path, depth+1)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				walk(v.Field(i), path+"."+v.Type().Field(i).Name, depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		case reflect.Map:
			iter := v.MapRange()
			for iter.Next() {
				walk(iter.Value(), fmt.Sprintf("%s[%v]", path, iter.Key()), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(root), "doc", 0)
}
