# Models in Go

## Optionality

The reference Go lowering is ogen's generic-box scheme (`gen/ir/generics.go`), which independently
arrives at exactly these four states: required-nonnull → bare `T`; optional → `Opt[T]` (a
`{value T; set bool}` box, so presence is distinct from the zero value); nullable → `Nil[T]`; both →
`OptNil[T]`. A mature real-world generator needing all four first-class boxes is the strongest
external confirmation of INV8, and — decisively — ogen makes this decision in its *emitter*
(`boxType`), keeping `Required` and `Nullable` orthogonal in the schema model right up to that
point. That is Morphic's split precisely: orthogonal bits in the IR, boxed in refine. Two
Go-specific optimizations to inherit: reference types (slice/map/pointer) can encode one of the four
states in their own `nil` (ogen's `NilSemantic: invalid | optional | null`), saving a wrapper;
`explicit` protobuf presence → pointer/hazzer, `implicit` → `omitzero,omitempty`, so a zero is not
serialized.

Presence is stated in `encoding/json/v2` terms, and `omitempty` never keeps a presence state. v2
defines `omitempty` by the encoded JSON — a field is dropped when it would encode as `null`, `""`,
`{}` or `[]` — so it keeps a zero number or `false` and drops a pointer to an empty struct: a set
but empty value turns into an absent one, the silent conflation [the shared
rule](../../refine/models.md#optionality--nullability--presence-inv8) forbids. `omitzero` is defined
by the Go value and asks the type's `IsZero` method when it has one. The boxes implement `IsZero` as
"not set" beside their own `MarshalJSONTo`/`UnmarshalJSONFrom`, so a box field tagged `omitzero` is
omitted exactly when unset, and an `Opt[int]` set to `0` is written. The `implicit` tag carries both
options because each alone misses a zero: `omitempty` keeps `0` and `false`, and `omitzero` keeps an
empty non-nil slice or map.

## `User` in Go

The IR and the plan are in [TypePlan](../../plan/types.md#a-model-with-lifecycle-visibility).

- **refine.** `ProjectVisibilityShapes` emits `StructType{Name:"User"}` (read) and
  `StructType{Name:"UserCreate"}` (create). `LowerOptionality`: `name`, required and non-null, is a
  bare `string`, and the optional `id`, `email` and `password` are `Opt[T]` boxes
  ([optionality](#optionality)), so an absent email stays apart from an empty one. `RenderCasing`
  turns `["email"]`→`Email`, and the tag is `Tag:json:"email,omitzero"`: the name from `WireName`,
  never the identifier, and `omitzero` because an unset box written without it would send
  `"email":null`. `password` is `Secret`, so `User`'s `String()`, `GoString()` and `LogValue()` show
  it as `[REDACTED]`, as they would a member whose type is `Sensitive`
  ([redaction](../../refine/redaction.md)), and it is excluded from the read shape. `LogValue` is
  what `log/slog` reads: its JSON handler writes any other value as JSON, the wire form, and
  resolves a `slog.LogValuer` only on the value it is handed, never on a list's elements, so
  `LogValue` writes each redacted position as the marker itself. All three take a value receiver: a
  pointer receiver's methods are not in a `User`'s method set, so `fmt` and `log/slog` would print
  field by field a `User` held by value, as `ListUsers` yields one and a slice holds one. `User`
  declares `Format(fmt.State, rune)` beside them, writing what `String` does, since `fmt` asks
  `String` and `GoString` under some verbs alone: measured with Go 1.27.3, `%d` of a model declaring
  the three printed each field, the secret in full, and of 58 renderings 27 held it, where with
  `Format` the 7 left were `%p` of a value, under which `fmt` asks no method, and the encoders,
  which read fields. `Additional:"closed"` → the model's `UnmarshalJSONFrom` decodes its members
  under `json.RejectUnknownMembers(true)`. Every model pins that option for itself ([the shared
  rule](../../refine/models.md#unknown-members)), `false` unless closed: a v2 option reaches every
  value below the call that sets it, so a closed model would otherwise make an open model nested in
  it strict, and a strict caller would reach a model the spec leaves open. A catch-all
  (`AdditionalProps`) is the struct's one embedded fallback, a `map[string]T` tagged
  `json:",embed"`, which receives the undeclared members whatever the options say.
- **emit.** Printer folds the two structs, prints through `go/format`. File `user.go` ([file
  names](naming.md#file-names)) in the root unit ([output units](../../refine/output-units.md)).
  Manifest records `User.ID → "User"` and `User.ID#create → "UserCreate"`, the shape's key ([the
  declaration a shape takes](../../plan/types.md#the-declaration-a-shape-takes)).

## A shape that is an alias

A shape whose projection coincides with the read one
([TypePlan](../../plan/types.md#the-declaration-a-shape-takes)) is `type UserCreate = User`, in the
file of `User`, and each signature names the alias: `go doc` shows `func Create(b shared.UserCreate)
string`. A caller in another module that built `shared.UserCreate{Name: "Ada"}` compiled before and
after `UserCreate` became a struct of its own, where one that built a `shared.User` for the same
call stopped with `cannot use … as shared.UserCreate value` (Go 1.27.3). T-60 holds it.

## Nesting

json/v2 refuses a value nested past 10,000 levels, decoding and encoding (`jsontext: exceeded max
depth`), and no option moves that bound, which is where the range of [the
limit](../../refine/models.md#how-deep-a-value-nests) ends. Below it the runtime counts for itself:
before it decodes a text and after it encodes one it scans the bytes once, counting the objects and
arrays open outside a string, and refuses one nested past `MaxDepth`, a response as a body that does
not decode and a request with an error, nothing sent. The scan took 1.6 ms on a 1.1 MB body that
json/v2 decoded in 6.7 ms, and a value at json/v2's bound decoded in 3 ms on a goroutine's stack,
which grows as it needs (Go 1.27.3, darwin/arm64). A count kept in each model's `UnmarshalJSONFrom`
would miss an `any` position, which json/v2 decodes with no code of the SDK's.

A caller can build a value that holds itself, through a pointer, a slice or a map. Encoding refuses
it (`encountered a cycle`). Printing must not follow it: Go ends the process on a stack overflow
(`fatal error: stack overflow`), which no `recover` stops, and a `String` calling each nested
value's `String` did so on a model whose pointer led back to it. So each model, union struct,
wrapper and positional struct declares `Format(fmt.State, rune)`, which `fmt` asks before `String`
and for every verb, and renders its members itself, calling a nested value's `Format` with a state
that carries the level; at `MaxDepth` levels it writes `...` and returns. `String` and `GoString`
print through it and `LogValue` returns its text, so all three end. Measured so, a value holding
itself through a pointer, a slice and a map each printed 64 levels and stopped, as did one held from
another unit, and `%d`, `%x` and `%q` showed a redacted member as `%v` does. The state is a type of
the module's own under `internal/`, which no caller can name, and `Format` is a name no member takes
([reserved names](naming.md#reserved-names)), as `String` is: 20 properties of the golden spec are
named `format` (spec @bc6fad24). Go generates no hash and no comparison of a model: `==` compares a
pointer by identity and is not defined over a slice or a map. T-63 holds these.

## Recursive references

Go holds a struct field by value, so a cycle of struct fields is an invalid recursive type. A slice,
a map, a pointer and an interface each break a cycle, so a list, a map, a union and a polymorphic
base — the last two a struct holding a sealed interface, or a pointer per variant where the union is
inclusive ([unions](unions.md)) — already do; a model, a tuple's positional struct and the boxes
above hold their contents by value, and do not. So a reference is held through a pointer when it
lies on a cycle of references Go holds by value, read after `Base` and `Mixins` are flattened into
the declarations they land in. The [shared refine
helper](../../refine/models.md#recursive-references) finds those cycles. The pointer stands for a
box's state where it can: a required reference is `*T`, which `Validate` reports when nil; an
optional or a nullable one is `*T`, nil meaning absent or null; and an optional nullable one is
`Opt[*T]`.

Go breaks every reference on such a cycle rather than the fewest that would do. The fewest depend
on the whole graph — one type added elsewhere can change which reference is picked — and each pick
that moves changes a field's type for every caller. Nor does it break every by-value reference whose
two ends merely share a component: a cycle that passes through a list, a map or a union already
compiles, and a pointer on it would ask every caller to check for nil where nothing requires it.
A reference is a pointer exactly when a by-value cycle passes through it, which only a change to
that cycle moves. T-20 holds the rule.
