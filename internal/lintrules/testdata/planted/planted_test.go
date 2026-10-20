package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var _ = json.Valid

// plantTestExemptions asserts by force, prints and swaps a process global,
// which a test file may do.
func plantTestExemptions() {
	var v any = 1
	_ = v.(int)
	println("test output")
	saved := os.Args
	os.Args = saved
}

// TestPlanted draws testifylint's finding, beside the forms of the checkers
// .golangci.yml turns off, which draw nothing.
func TestPlanted(t *testing.T) {
	xs := []int{1}
	assert.Equal(t, 1, len(xs)) // want "len: use assert.Len (testifylint)"
	var err error
	assert.NoError(t, err)
	encoded, ratio := `{"a":1}`, 1.5
	assert.Equal(t, `{"a":1}`, encoded)
	assert.Equal(t, 1.5, ratio)
	assert.Equal(t, xs, []int{1})
	var none any
	assert.Equal(t, nil, none)
}
