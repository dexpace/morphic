package oas

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTemplateFault_NamesWhatIsWrong pins the wording of the template check.
func TestTemplateFault_NamesWhatIsWrong(t *testing.T) {
	assert.Empty(t, templateFault("https://{a}/{b}"))
	assert.Contains(t, templateFault("{a}/{a}"), `"a"`)
	assert.Contains(t, templateFault("{a"), "no }")
}
