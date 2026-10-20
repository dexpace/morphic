package ports

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"

	"github.com/dexpace/morphic/ir"
)

var _ = json.Valid

// Registry builds a registry, which only js builds and cmd/morphic-vet
// refuses.
func Registry() ir.TypeRegistry { return ir.TypeRegistry{} }
