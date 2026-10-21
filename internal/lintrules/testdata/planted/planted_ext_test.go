package planted_test

import "encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"

var _ = json.Valid
