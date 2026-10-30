// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package tooling stands for a package outside the pipeline, a command or a
// test harness: invariant 5's output rules do not reach it, and the rule that
// recovers a panic through barrier.Catch alone does.
package tooling

import "fmt"

// plantTooling prints, which a tool may, and recovers a panic by hand, which no
// package may.
func plantTooling() {
	fmt.Println("a tool may print")
	defer func() { _ = recover() }() // want "use of `recover` forbidden because \"recover a panic through barrier.Catch\" (forbidigo)"
}
