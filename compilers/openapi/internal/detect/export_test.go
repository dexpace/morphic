// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect

import (
	"context"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
)

// DeclaresBlockKey is declaresBlockKey, for the external tests.
var DeclaresBlockKey = declaresBlockKey

// DeclaresProbeKey is declaresProbeKey, for the external tests.
var DeclaresProbeKey = declaresProbeKey

// DecodeYAML is decodeYAML with no node budget, for the external tests.
func DecodeYAML(ctx context.Context, data []byte) (sniffProbe, *load.Parsed, error) {
	return decodeYAML(ctx, data, budget.Unbounded())
}

// DocumentRoot is documentRoot, for the external tests.
var DocumentRoot = documentRoot

// MajorMinor is majorMinor, for the external tests.
var MajorMinor = majorMinor

// MaxVersionLines is maxVersionLines, for the external tests.
const MaxVersionLines = maxVersionLines

// ProbeFromMapping is probeFromMapping, for the external tests.
var ProbeFromMapping = probeFromMapping

// Sniff is sniff with no node budget, for the external tests.
func Sniff(ctx context.Context, data []byte) (sniffProbe, *load.Parsed, error) {
	return sniff(ctx, data, budget.Unbounded())
}

// SniffProbe is sniffProbe, for the external tests.
type SniffProbe = sniffProbe
