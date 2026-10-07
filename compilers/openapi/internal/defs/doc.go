// Package defs reads a "#/$defs/..." pointer the way the resolver reads one for
// a reference it meets on its own walk: relative to the schema that spells it,
// not to the document (GitHub #557).
//
// It is its own package because both sides of the compiler need the one rule:
// load, which hands the resolver the definition the rule names so every reader
// of the resolved model agrees, and the lowering, which derives each target's
// identity from where that definition is written.
package defs
