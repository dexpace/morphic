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
  `"email":null`. `password` (`Secret`) gets a redacting `String()` and is excluded from the read
  shape. `Additional:"closed"` → the model's `UnmarshalJSONFrom` decodes its members under
  `json.RejectUnknownMembers(true)`. Every model pins that option for itself, `false` unless closed:
  a v2 option reaches every value below the call that sets it, so a closed model would otherwise
  make an open model nested in it strict, and a strict caller would reach a model the spec leaves
  open. A catch-all (`AdditionalProps`) is the struct's one embedded fallback, a `map[string]T`
  tagged `json:",embed"`, which receives the undeclared members whatever the options say.
- **emit.** Printer folds the two structs, prints through `go/format`. File `user.go` in the root
  unit ([output units](../../refine/output-units.md)). Manifest records `User.ID → "User"` and
  `User.ID#create → "UserCreate"`.

## Recursive references

Go holds a struct field by value, so a cycle of struct fields is an invalid recursive type. A slice,
a map, a pointer and an interface each break a cycle, so a list, a map, a union and a polymorphic
base — the last two a struct holding a sealed interface ([unions](unions.md)) — already do; a model,
a tuple's positional struct and the boxes above hold their contents by value, and do not. So a
reference is held through a pointer when it lies on a cycle of references Go holds by value, read
after `Base` and `Mixins` are flattened into the declarations they land in. The [shared refine
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
