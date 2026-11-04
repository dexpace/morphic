// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// Owned is a value of a Schema Object field the check leaves to its caller
// (Options.Owned): the field, the value written for it, and where the schema
// writing it is read in its document, Options.Root included. A finding about
// it is placed as the check places its own (Finding).
type Owned struct {
	Field  string
	Value  *yaml.Node
	Schema jsontext.Pointer
	// at is where the value is read below the root, which locate places.
	at     jsontext.Pointer
	locate Locator
	rootAt jsontext.Pointer
	readAt bool
}

// Finding returns the error format and args state about o, placed at the
// value as the check places a finding of its own, naming where o was read
// where an alias or a merge key places it elsewhere (Options.ReadAt).
func (o Owned) Finding(code, format string, args ...any) ir.Diagnostic {
	d := diag.Newf(ir.SeverityError, code, o.locate(o.at), format, args...)
	if read := o.rootAt + o.at; o.readAt && d.Provenance.Pointer != "" && d.Provenance.Pointer != read {
		d.Message += readAtClause + string(read)
	}
	return d
}
