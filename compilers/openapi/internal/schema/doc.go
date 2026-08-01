// Package schema lowers OpenAPI schemas into IR types: the shape walk itself,
// the compositions written around it, the references that reach other schemas,
// and the preservation of what the IR has no field for.
//
// It is one package because those are one cycle. Lowering a schema resolves the
// references inside it, resolving a reference lowers what it names, and a
// composition is lowered by lowering its branches — so no line can be drawn
// through the set that some call does not cross back over
// (micro-compiler-design §5). The mutual recursion is pinned, by name, in
// internal/archtest.
//
// Its exported surface is the entry points the rest of the compiler needs, plus
// the few facts a carrier lowering has to agree with this one about. Everything
// the walk says to itself stays unexported, so a caller cannot enter it halfway
// down — which is why the recursion pinned in internal/archtest is almost
// entirely unexported names.
package schema
