// Package ynode spells yaml.v3 nodes for tests: the constructors for the node
// kinds a parse produces, the `<<` merge key among them, and the merge chain the
// compiler's depth bounds are measured against.
//
// It imports nothing of the compiler, so any package's internal tests can build
// trees with it. That includes nodeview's, which a home above nodeview could not
// serve: an internal test file cannot import a package that imports its own.
package ynode
