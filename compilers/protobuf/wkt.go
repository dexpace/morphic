package protobuf

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/ir"
)

// Go import paths of the well-known runtime packages, recorded on External nodes
// so an emitter can resolve them to library types.
const (
	pkgKnown     = "google.golang.org/protobuf/types/known"
	pkgWrappers  = pkgKnown + "/wrapperspb"
	pkgEmpty     = pkgKnown + "/emptypb"
	pkgFieldMask = pkgKnown + "/fieldmaskpb"
)

// messageOrWKT resolves a message-typed reference. Well-known types map to the
// faithful IR node (a date/time primitive, Any, a nullable primitive, or an
// External); every other well-known type from google/protobuf preserves as an
// External so its internals are never hoisted; user messages hoist normally.
func (l *lowerer) messageOrWKT(md protoreflect.MessageDescriptor) ir.TypeRef {
	fn := string(md.FullName())
	if ref, ok := l.wellKnown(fn); ok {
		return ref
	}
	if isWellKnownFile(md.ParentFile()) {
		return l.externalRef(fn, pkgKnown)
	}
	return l.messageRef(md)
}

// wellKnown maps a recognized google.protobuf type to its IR lowering, reporting
// false for types with no special mapping.
func (l *lowerer) wellKnown(fn string) (ir.TypeRef, bool) {
	switch fn {
	case "google.protobuf.Timestamp":
		return l.primRef(ir.PrimDatetime), true
	case "google.protobuf.Duration":
		return l.primRef(ir.PrimDuration), true
	case "google.protobuf.Any",
		"google.protobuf.Struct",
		"google.protobuf.Value",
		"google.protobuf.ListValue",
		"google.protobuf.NullValue":
		return l.anyRef(), true
	case "google.protobuf.Empty":
		return l.externalRef(fn, pkgEmpty), true
	case "google.protobuf.FieldMask":
		return l.externalRef(fn, pkgFieldMask), true
	}
	if prim, ok := wrapperPrim(fn); ok {
		return l.wrapperRef(fn, prim), true
	}
	return ir.TypeRef{}, false
}

// wrapperRef lowers a google.protobuf wrapper per the configured policy: a
// nullable primitive (default) or an External box.
func (l *lowerer) wrapperRef(fn string, prim ir.PrimKind) ir.TypeRef {
	if l.opts.Wrappers == WrapperExternal {
		return l.externalRef(fn, pkgWrappers)
	}
	ref := l.primRef(prim)
	ref.Nullable = true
	return ref
}

// wrapperPrim maps a google.protobuf wrapper type to its underlying primitive.
func wrapperPrim(fn string) (ir.PrimKind, bool) {
	switch fn {
	case "google.protobuf.DoubleValue":
		return ir.PrimFloat64, true
	case "google.protobuf.FloatValue":
		return ir.PrimFloat32, true
	case "google.protobuf.Int64Value":
		return ir.PrimInt64, true
	case "google.protobuf.UInt64Value":
		return ir.PrimUint64, true
	case "google.protobuf.Int32Value":
		return ir.PrimInt32, true
	case "google.protobuf.UInt32Value":
		return ir.PrimUint32, true
	case "google.protobuf.BoolValue":
		return ir.PrimBool, true
	case "google.protobuf.StringValue":
		return ir.PrimString, true
	case "google.protobuf.BytesValue":
		return ir.PrimBytes, true
	default:
		return "", false
	}
}

// externalRef interns an External node for a well-known library type identified
// by its full proto name, and returns a reference to it.
func (l *lowerer) externalRef(identity, pkg string) ir.TypeRef {
	id := ir.TypeID("t/protobuf/external/" + identity)
	if _, ok := l.out.Types[id]; !ok {
		l.out.Types[id] = &ir.External{
			TypeCommon: ir.TypeCommon{
				ID:         id,
				Name:       ir.Naming{Source: identity, Canonical: canonicalWords(lastSegment(identity))},
				Provenance: ir.Provenance{Source: l.srcIndex, Pointer: identity},
			},
			Identity: identity,
			Package:  pkg,
		}
	}
	return ir.TypeRef{Target: id}
}

// isWellKnownFile reports whether a file descriptor is one of the bundled
// google/protobuf well-known-type definitions.
func isWellKnownFile(fd protoreflect.FileDescriptor) bool {
	return strings.HasPrefix(fd.Path(), "google/protobuf/")
}

// lastSegment returns the final dot-separated segment of a qualified name.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
