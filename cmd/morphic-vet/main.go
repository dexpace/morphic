// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/dexpace/morphic/internal/lintrules"
)

// vet is the go vet tool's entry point, which a test replaces to run main in
// its own process: a child's coverage is discarded (internal/exectest).
var vet = unitchecker.Main

func main() {
	vet(lintrules.Analyzers()...)
}
