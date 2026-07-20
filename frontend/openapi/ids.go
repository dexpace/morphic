package openapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ptr joins segments into an RFC 6901 JSON pointer. IDs are derived from these
// pointers (ir-design §3.1); no other code may construct IDs or pointers.
func ptr(segments ...string) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteByte('/')
		b.WriteString(escapeSegment(seg))
	}
	return b.String()
}

// escapeSegment applies RFC 6901 escaping: ~ first, then /.
func escapeSegment(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// namedTypeID returns the stable ID of a components-named schema at pointer.
func namedTypeID(pointer string) ir.TypeID { return ir.TypeID("t/openapi" + pointer) }

// anonTypeID returns the stable ID of a hoisted inline type at pointer.
func anonTypeID(pointer string) ir.TypeID { return ir.TypeID("t/anon" + pointer) }

// primTypeID returns the interned ID of primitive kind k.
//
//nolint:unused // identity seam consumed by later frontend files
func primTypeID(k ir.PrimKind) ir.TypeID { return ir.TypeID("t/prim/" + string(k)) }

// opID returns the stable ID of the operation at pointer.
//
//nolint:unused // identity seam consumed by later frontend files
func opID(pointer string) ir.OpID { return ir.OpID("op/openapi" + pointer) }

// propID returns the stable ID of the property at pointer.
//
//nolint:unused // identity seam consumed by later frontend files
func propID(pointer string) ir.PropID { return ir.PropID("p/openapi" + pointer) }

// authIDFor returns the stable ID of the named security scheme.
//
//nolint:unused // identity seam consumed by later frontend files
func authIDFor(name string) ir.AuthID {
	return ir.AuthID("auth/openapi" + ptr("components", "securitySchemes", name))
}

// serviceID returns the stable ID of the service for the given source index.
//
//nolint:unused // identity seam consumed by later frontend files
func serviceID(sourceIndex int) ir.ServiceID {
	return ir.ServiceID("s/openapi/" + strconv.Itoa(sourceIndex))
}

// refTypeID maps a $ref string to the stable ID of its target.
func refTypeID(ref string) (ir.TypeID, error) {
	doc, pointer, found := strings.Cut(ref, "#")
	if !found && doc == "" {
		return "", fmt.Errorf("openapi: empty $ref")
	}
	switch {
	case doc != "": // external document
		return ir.TypeID("t/openapi/ext/" + ref), nil
	case pointer != "":
		// Share the interning discriminator so a $ref derives the same ID the
		// target was interned under: named only for a top-level component schema
		// (/components/schemas/<name>), anonymous for any deeper pointer.
		return typeIDForPointer(pointer), nil
	default:
		return "", fmt.Errorf("openapi: unsupported $ref form %q", ref)
	}
}
