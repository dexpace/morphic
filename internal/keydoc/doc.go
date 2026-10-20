// Package keydoc reads the repository's keyed documents: Markdown pages whose
// entries are third-level headings naming a stable key, each closing with a
// list of named fields, and the citations other files make of an entry.
//
// docs/parser-library.md and docs/compilers/openapi.md are both such pages,
// and the tests that hold each to the code read it through this package, so no
// two of them read the grammar apart. What a field means is the reading test's
// to say: this package reads its name, its value and the lines it runs on over.
package keydoc
