// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/ir"
)

// Expansion exposes the reference-expansion count to this package's external
// tests, which drive it with trees a document would take a compile to build.
type Expansion = expansion

// NewExpansion exposes newExpansion.
func NewExpansion(l budget.Limit) *Expansion { return newExpansion(l) }

// Admit exposes admit.
func (e *expansion) Admit(at bind.Reading, roots ...*yaml.Node) bool { return e.admit(at, roots...) }

// Refusal exposes refusal.
func (e *expansion) Refusal(srcIndex int) (ir.Diagnostic, bool) { return e.refusal(srcIndex) }

// Spent is what the copies admitted so far were charged.
func (e *expansion) Spent() int { return e.spent.Spent() }

// Weighed counts the declarations weighed so far, once for each reading.
func (e *expansion) Weighed() int { return len(e.weights) }

// WeightOf exposes weightOf.
func WeightOf(root *yaml.Node, at bind.Reading, within budget.Limit) int {
	return weightOf(root, at, within)
}

// ReadingOf exposes readingOf.
func ReadingOf(c Ctx, usePtr jsontext.Pointer) bind.Reading { return readingOf(c, usePtr) }
