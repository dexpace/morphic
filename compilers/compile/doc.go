// Package compile holds the state every spec compiler needs and the invariants
// that state carries, so each compiler does not reimplement them.
//
// It owns what every compiler must agree on, and nothing else. It imports only
// ir.
//
//   - The type registry with the source-coordinate map that keeps invariant 3
//     true (stable IDs; one node per source coordinate), and the namespace rule
//     that comes with it: a node a lowering mints takes a namespace no source
//     coordinate addresses.
//   - Diagnostic accumulation with identity dedup.
//   - The canonical naming grammar, which invariant 4 makes a property of the IR
//     rather than of a compiler.
//   - The identifier grammar: the kind prefix that opens an ID and the namespace
//     that follows it.
//
// The package boundary is the point. Architecture tests assert that no package
// outside this one writes to an ir.TypeRegistry, derives a canonical name, or
// builds an ID from a string, and rules like those are inexpressible without an
// outside — which is why a package this small is worth its own directory.
//
// What deliberately stays with the compiler: ir.Document assembly (seventeen of
// its eighteen fields carry no framework invariant), recursion depth bounds (the
// right cap for a JSON Schema walk is not the right one for an SDL walk), and
// the derivation an ID's path comes from — a JSON Pointer, a GraphQL structural
// path and a protobuf fully-qualified name are different things, and the format
// that computes one is the only place that can. Promoting a borderline item here
// later is additive; demoting one is a breaking change across every compiler, so
// borderline items start outside.
package compile
