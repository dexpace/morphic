// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap

import (
	"errors"
	"maps"

	"github.com/dexpace/morphic/ir"
)

// ErrInvalidTable reports a type table breaking a rule every target holds it to.
var ErrInvalidTable = errors.New("invalid type table")

// Table maps the IR's leaf types to one target's types. It is plain data,
// which the functions here only read, so a table may be read concurrently.
type Table struct {
	// Primitives holds the rows by primitive, then by encoding name; the
	// encoding "" is the primitive's default row.
	Primitives map[ir.PrimitiveKind]map[string]Row `json:"primitives,omitempty"`
	// Externals holds the rows by ir.External's Identity.
	Externals map[string]Row `json:"externals,omitempty"`
}

// Row is the target type a key maps to, with what writing it needs.
type Row struct {
	// Type is the type as the target spells it in a file.
	Type string `json:"type"`
	// Codec reads and writes the type, "" for the type's own.
	Codec string `json:"codec,omitempty"`
	// Imports maps each name Type and Codec spell to what a file imports for
	// it; empty is none.
	Imports map[string]string `json:"imports,omitempty"`
	// Dependency is what the SDK's build requires for the type; nil is the
	// standard library or the SDK's own runtime.
	Dependency *Dependency `json:"dependency,omitzero"`
}

// Dependency is a module the SDK's build requires for a row's type.
type Dependency struct {
	// Module is a Go module path or a Maven coordinate.
	Module string `json:"module"`
	// MinVersion is the lowest version the SDK is generated against.
	MinVersion string `json:"minVersion"`
}

// copyRow returns row sharing nothing with it: its Imports cloned, nil staying
// nil, and its Dependency pointing at a copy.
func copyRow(row Row) Row {
	copied := row
	copied.Imports = maps.Clone(row.Imports)
	if row.Dependency != nil {
		dependency := *row.Dependency
		copied.Dependency = &dependency
	}
	return copied
}

// PrimitiveKey is the key of a primitive's row.
type PrimitiveKey struct {
	// Primitive is the row's primitive.
	Primitive ir.PrimitiveKind
	// Encoding is the encoding name, "" for the primitive's default row, as
	// the table's own key spells it.
	Encoding string
}
