// Package navigation reads a JSON pointer through the library's parsed model
// as jsonpointer.GetTarget's walk reads it, a token at a time, and says where
// that walk leaves the model for the raw YAML an object was built from.
//
// The library finds a key in raw YAML by comparing a mapping's keys in order,
// so a reader asking it about raw YAML once per reference pays the square of a
// wide mapping (GitHub #778). Knowing where the walk leaves the model lets a
// reader stop there, or read on through an index, without that scan.
package navigation
