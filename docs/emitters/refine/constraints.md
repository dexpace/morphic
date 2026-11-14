# Constraints and validation

`LowerConstraints` reads `Constraints` on properties, scalars, and lists
([constraints](../../ir/constraints.md)) and *builds the structure* of a per-type `Validate()`
method — which checks apply to which fields — as typed AST. That method is the one validator: no
field carries validation metadata, a `validate:"…"` struct tag or a registration, since nothing
generated reads one and a reflection validator following it is a second walker no nesting limit
bounds ([how deep a value nests](models.md#how-deep-a-value-nests)), which over such tags ended the
process on a Go value holding itself (go-playground/validator v10.30.2, Go 1.27.3). Validation is
therefore neither pure boilerplate (the checks depend on the spec) nor free-form template logic:
refine decides the set of checks, and the individual check primitives (`checkMinLength`,
`checkMultipleOf` on `big.Rat`) are runtime helpers. This is ogen's `oas_validators_gen.go` split,
relocated to honor the structure-vs-boilerplate line ([structure and
boilerplate](../emit/templates.md)). Numeric bounds flow through `BigVal`, **never float64** — ogen
itself gets storage right yet leaks `Float64()` in its `minNum`/`maxNum` merge path, so any
constraint intersection the plan or a refiner performs (a scalar chain's bounds, `allOf` bounds)
compares them by `ir.Decimal`, never by a float or a rational, which cannot build `1e1000001`. A
`Pattern` a target's regex engine cannot honor is dropped with a `warning` diagnostic, never
silently mistranslated, and a constraint on a type only a user's type-table row knows is left to the
server with an info diagnostic ([type table](../contract/type-table.md)).

`LowerConstraints` also reads `Operation.ParamChoices` into the request type's `Validate()`: one
check per choice, failing where the fields set belong to no single variant, or to none where the
choice is `Required`, and naming the choice and its variants
([choices among parameters](../../ir/operations.md#choices-among-parameters)).
