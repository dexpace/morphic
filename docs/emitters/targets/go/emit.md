# Go's target AST and printer

## The target AST

```go
package goast

type Node interface { node(); Kind() NodeKind; Origin() emitter.EntityID }

type Module struct { Packages []*Package }
type StructType struct {
    Common
    Fields  []*Field
    Embeds  []Expr           // the catch-all map (§8.1); what Base, Implements and Mixins
                             // declare is flattened into Fields (TypePlan.Flattened)
}
type Field struct {
    Common
    Name Ident               // cased identifier
    Type Expr                // a typed expression node — cannot be a raw string
    Tag  StructTag           // built from WIRE name/ID/format, NEVER the identifier
    Opt  Optionality
}
type InterfaceType struct { Common; Methods []MethodSig; Marker string } // union base / abstract model
type Method struct { Common; Name Ident; Params []*Param; Returns Expr; Kind MethodKind }

type Common struct {
    origin emitter.EntityID   // ← the IR ID this node came from (manifest / surface / prune; INV3)
    prov   ir.Provenance      // passthrough for diagnostics + Inferred marking
    ident  Ident              // NEUTRAL until RenderCasing runs: naming.Words(Naming), never cased
}

type Expr interface{ isExpr() }
type Named   struct { Pkg, Name string }   // time.Time, uuid.UUID — both standard library (§8)
type Pointer struct { Elem Expr }
type Slice   struct { Elem Expr }
type MapExpr struct { Key, Value Expr }
type Iface   struct{}                       // any
```

## Why a domain AST → text → `go/format`, not `go/ast` directly

`go/ast` is awkward to *synthesize*
(it wants a `token.FileSet` and real positions and cannot carry generator intent like a field's
`Origin` IR ID or a `{t:TypeID}` doc token). The domain AST is smaller, holds provenance and wire
metadata on its nodes, and prints to text that `go/format` then makes canonical. `go/ast` is used in
exactly one place — re-parsing *existing* files for additive merge (§11) — where its parser is the
right tool. The two Go trees are kept aligned by ID-keying: "a symbol I generated" ↔ "a symbol I
parsed" is a lookup.

## Runtime templates

The Go templates lean on the standard library wherever Go 1.27 does the work (§8). In particular the
transport template does not drain an unread response body by hand so that its connection can be
reused: `net/http` does that itself on `Close`, up to a bound it sets, and past the bound closes the
connection rather than read an arbitrarily large body.
