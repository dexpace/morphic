// Package planted holds what the rules .golangci.yml configures exist to
// refuse, one finding to a line, each line marked with the message it must
// draw. scripts/lint-planted.sh requires exactly those findings.
package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

var (
	_ = json.Valid
	_ = jsontext.Value(nil)
	_ = jsonv2.Marshal
)
