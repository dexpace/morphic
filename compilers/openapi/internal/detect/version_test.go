package detect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMajorMinor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3.1", majorMinor("3.1.0"))
	assert.Equal(t, "3.1", majorMinor("3.1"))
	assert.Equal(t, "4", majorMinor("4"))
}
