// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/dexpace/morphic/internal/lintrules"
)

func main() {
	unitchecker.Main(lintrules.Analyzers()...)
}
