package operation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentTypeKeys_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, contentTypeKeys(nil))
}
