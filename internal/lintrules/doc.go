// Package lintrules holds the lint rules this repository writes for itself:
// go/analysis analyzers for what no off-the-shelf linter expresses, each about
// which package may build or spell a part of the IR. cmd/morphic-vet runs them
// under go vet, on every port make build covers.
//
// Each rule reads types, not spellings, so an aliased import, a value held in a
// local or a promoted method is still what it is. The tests plant every finding
// a rule exists to draw, beside what it must leave alone, in testdata/mod, a
// tree laid out like the module.
package lintrules
