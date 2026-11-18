// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// stand is where the node a schema lowers to stands where that is not where the
// schema is written: at, the pointer of of, the position whose allOf the schema
// is the inline principal of, as the node the position owns stands for the
// whole conjunction (docs/compilers/openapi.md#allof-refinement). The zero
// stand is the schema's own. It is a parameter, never read off the schema or
// the context, since a $ref to the entry lowers the same schema at its own
// pointer.
type stand struct {
	of *statement.Schema
	at jsontext.Pointer
}

// pointer returns where the node of a schema written at written stands.
func (st stand) pointer(written jsontext.Pointer) jsontext.Pointer {
	if st.of == nil {
		return written
	}
	return st.at
}
