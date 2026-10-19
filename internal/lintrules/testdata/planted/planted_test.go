package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"

	"github.com/dexpace/morphic/ir"
)

var _ = json.Valid

// fixture is a registry a test file builds and writes, which draws nothing.
var fixture = ir.TypeRegistry{}

func init() { fixture["a"] = nil }
