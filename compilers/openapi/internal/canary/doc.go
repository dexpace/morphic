// Package canary pins the parser library behaviours docs/parser-library.md
// records, one test for each entry. Each test asserts what the library does
// today, defects included, so a library change that alters a behaviour the
// compiler relies on, or guards against, turns it red.
//
// The package holds no code: its tests call the library directly, and the
// ones whose call may end the process, or never return, run it in a child
// process.
package canary
