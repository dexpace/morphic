package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/dexpace/morphic/internal/lintrules"
)

func main() {
	unitchecker.Main(lintrules.Analyzers()...)
}
