// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/ir"
)

// Expansion exposes the reference-expansion count to this package's external
// tests, which drive it with trees a document would take a compile to build.
type Expansion = expansion

// NewExpansion exposes newExpansion.
func NewExpansion(limit budget.Limit) *Expansion { return newExpansion(limit) }

// Admit exposes admit.
func (e *expansion) Admit(at reftable.Reading, end *yaml.Node, hops ...*yaml.Node) bool {
	return e.admit(at, end, hops...)
}

// Refusal exposes refusal.
func (e *expansion) Refusal(srcIndex int) (ir.Diagnostic, bool) { return e.refusal(srcIndex) }

// Spent is what the copies admitted so far were charged.
func (e *expansion) Spent() int { return e.spent.Spent() }

// Weighed counts the declarations weighed so far, once for each reading.
func (e *expansion) Weighed() int { return len(e.weights) }

// ReferenceExpansionSize exposes referenceExpansionSize.
func ReferenceExpansionSize(root *yaml.Node, at reftable.Reading, within budget.Limit) int {
	return referenceExpansionSize(root, at, within)
}

// ReadingOf exposes readingOf.
func ReadingOf(serviceContext Ctx, usePtr jsontext.Pointer) reftable.Reading {
	return readingOf(serviceContext, usePtr)
}
