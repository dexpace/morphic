package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"
)

var _ = json.Valid

// A test file may assert by force and print, which draws nothing.
func init() {
	var v any = 1
	_ = v.(int)
	println("test output")
}
