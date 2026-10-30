// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var _ = json.Valid

// plantTestExemptions asserts by force, prints, swaps a process global and
// recovers a panic by hand, which a test file may do.
func plantTestExemptions() {
	defer func() { _ = recover() }()
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
