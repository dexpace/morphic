// Command morphic is the Morphic CLI: it lowers an API spec into Morphic IR.
//
// It parses flags, runs the engine and turns the run into an exit code. What
// it prints is the engine's to produce: engine/report renders the diagnostics
// and --explain, and engine/output writes the document. The CLI chooses only
// the streams; every stage below the engine emits typed ir.Diagnostic values
// and never writes to stderr (architecture §4).
package main
