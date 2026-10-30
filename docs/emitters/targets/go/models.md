### 8.1 A model with lifecycle visibility

IR: `Model{ Properties:[id (readOnly), name (required), email, password (writeOnly, Secret)],
Additional:"closed" }`.

- **plan.** `ModelShape(User, read)` = {id,name,email}; `ModelShape(User, create)` =
  {name,email,password}. `Flattened` walks `Base`, `Implements` and `Mixins` (none here). That
  `User` is both written and read is the derived-fact index's, `Facts.Usage(User)` = Input|Output,
  which no plan field copies (§3.6).
- **refine.** `ProjectVisibilityShapes` emits `StructType{Name:"User"}` (read) and
  `StructType{Name:"UserCreate"}` (create). `LowerOptionality`: `name`, required and non-null, is a
  bare `string`, and the optional `id`, `email` and `password` are `Opt[T]` boxes (§4.7), so an
  absent email stays apart from an empty one. `RenderCasing` turns `["email"]`→`Email`, and the tag
  is `Tag:json:"email,omitzero"`: the name from `WireName`, never the identifier, and `omitzero`
  because an unset box written without it would send `"email":null`. `password` (`Secret`) gets a
  redacting `String()` and is excluded from the read shape. `Additional:"closed"` → the model's
  `UnmarshalJSONFrom` decodes its members under `json.RejectUnknownMembers(true)`. Every model pins
  that option for itself, `false` unless closed: a v2 option reaches every value below the call that
  sets it, so a closed model would otherwise make an open model nested in it strict, and a strict
  caller would reach a model the spec leaves open. A catch-all (`AdditionalProps`) is the struct's
  one embedded fallback, a `map[string]T` tagged `json:",embed"`, which receives the undeclared
  members whatever the options say.
- **emit.** Printer folds the two structs, prints through `go/format`. File `user.go` in the root
  unit (§4.17). Manifest records `User.ID → "User"` and `User.ID#create → "UserCreate"`.
