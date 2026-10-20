// Command morphic-census compiles API specs and reports which facts of the IR
// the resulting documents set, as internal/census counts them, with the count
// of documents and instances for each. The report lists every fact, so what
// no spec produces reads as a zero. It is tooling built on internal/census,
// not part of the compile pipeline.
//
// Each argument is a spec file or a directory of specs, chosen as
// morphic-harness chooses them. Exit code is 0 on a report, and 2 on a usage
// error or a failure to read a spec or the IR's declarations.
package main
