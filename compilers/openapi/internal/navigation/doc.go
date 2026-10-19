// Package navigation reads a JSON pointer through the library's parsed model
// as jsonpointer.GetTarget's walk reads it, and says where that walk leaves
// the model for the raw YAML an object was built from. Tests such as
// TestWalk_AnswersAsTheWholeReadDoes hold each reading to the library's.
//
// The library finds a key in raw YAML by comparing a mapping's keys in order,
// taking a repeated one's first (docs/parser-library.md#duplicate-key), so a
// reader asking it once per reference pays the square of a wide mapping
// (GitHub #778). Knowing where the walk leaves the model lets a reader stop
// there, or read on through an index.
package navigation
