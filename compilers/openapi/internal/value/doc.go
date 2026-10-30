// Package value classifies YAML scalars and lowers source values into
// ir.Value, keeping numeric literals as their exact source text so nothing
// rounds through float64.
//
// Classification is by wire spelling alone, independent of any surrounding
// schema type — the Type-vs-Value split — which is why the package needs nothing
// from the compiler above it. A mapping is read as the parser reads one, through
// nodeview, so a value and a payload kept verbatim name the same members.
package value
