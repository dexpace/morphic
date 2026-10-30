// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package diag

import (
	"errors"

	"github.com/dexpace/morphic/barrier"
)

// OneLine quotes err, an error carrying text the compiler did not write, for a
// diagnostic message, as barrier.Line renders it. A diagnostic is rendered one
// per line, so an embedded newline would split one report into several, each
// after the first lacking severity, code and location. A library's error obeys
// no size limit either: yaml.v3 reports a duplicated mapping key once per prior
// occurrence, so a 32 KB source repeating one key 6,553 times raises a 1.2 GB
// error.
func OneLine(err error) string {
	return barrier.Line(err)
}

// Catch runs f, a call of code the compiler does not own, behind barrier.Catch.
// It reports whether f panicked and, when it did, the panic value's text as
// OneLine quotes an error's, for the barrier calling it to word a diagnostic or
// an error of its own around. A nil f is reported as the error barrier.Catch
// refuses it with.
//
// It catches what barrier.Catch does and no more. The parser decodes on
// goroutines of its own (docs/parser-library.md#goroutine-per-node), where a
// panic ends the process whatever barrier its caller stands behind.
func Catch(f func()) (text string, panicked bool) {
	err := barrier.Catch(f)
	if err == nil {
		return "", false
	}
	if perr, ok := errors.AsType[*barrier.PanicError](err); ok {
		return perr.Text(), true
	}
	return OneLine(err), true
}
