// Package compile holds the state every spec compiler needs and the invariants
// that state carries, so each compiler does not reimplement them.
//
// It owns three things: the type registry together with the source-coordinate
// map that keeps invariant 3 true (stable IDs; one node per source coordinate),
// diagnostic accumulation with identity dedup, and the canonical naming grammar
// invariant 4 makes a property of the IR rather than of a compiler. Nothing else.
// It imports only ir.
//
// The package boundary is the point. Architecture tests assert that no package
// outside this one writes to an ir.TypeRegistry or derives a canonical name of
// its own, and rules like those are inexpressible without an outside — which is
// why a package this small is worth its own directory.
//
// What deliberately stays with the compiler: ir.Document assembly (seventeen of
// its eighteen fields carry no framework invariant), recursion depth bounds (the
// right cap for a JSON Schema walk is not the right one for an SDL walk), and
// the ID-derivation scheme (Intern takes the ID as a parameter). Promoting a
// borderline item here later is additive; demoting one is a breaking change
// across every compiler, so borderline items start outside.
package compile
