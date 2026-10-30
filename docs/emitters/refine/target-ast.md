### 4.3 The target AST substrate

The target AST (`emitters/golang/goast` for Go, `emitters/kotlin/ktast` for both Kotlin targets) is
a small, purpose-built, sealed-sum syntax layer — the exact code the SDK needs, at
declaration/signature granularity — modelled with the same discipline the IR uses (INV
Go-conventions): an unexported marker method, one struct per node kind, a `Kind()` accessor, a
switch-completeness test over the node enum, its kind list held to what the package declares
(`internal/decls`), as the IR's is.

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

Two properties make this the right substrate: **illegal code is unrepresentable** (a field's type is
an `Expr`, not a string; a union variant that forgot its wire tag is a missing struct-literal field
the Go compiler flags inside Morphic — the antidote to the Mustache `oneOf`/`allOf` failure class),
and **every node carries its `Origin`**, so the manifest, import graph, and surface model are one
walk (§11, §12).
