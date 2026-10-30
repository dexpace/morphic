# Constraints and validation

`LowerConstraints` reads `Constraints` on properties, scalars, and lists (§5.3 of `ir-design.md`)
and does two things: it stamps field-level validation metadata (a `validate:"…"` struct tag or an
equivalent registration) and it *builds the structure* of a per-type `Validate()` method — which
checks apply to which fields — as typed AST. Validation is therefore neither pure boilerplate (the
checks depend on the spec) nor free-form template logic: refine decides the set of checks, and the
individual check primitives (`checkMinLength`, `checkMultipleOf` on `big.Rat`) are runtime helpers.
This is ogen's `oas_validators_gen.go` split, relocated to honor the structure-vs-boilerplate line
(§5.2). Numeric bounds flow through `BigVal`, **never float64** — ogen itself gets storage right yet
leaks `Float64()` in its `minNum`/`maxNum` merge path, so any constraint intersection the plan or a
refiner performs (a scalar chain's bounds, `allOf` bounds) compares them by `ir.Decimal`, never by a
float or a rational, which cannot build `1e1000001`. A `Pattern` a target's regex engine cannot
honor is dropped with a `warning` diagnostic, never silently mistranslated, and a constraint on a
type only a user's type-table row knows is left to the server with an info diagnostic (§2.6).
