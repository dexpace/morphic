// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentTypeKeys_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, contentTypeKeys(nil))
}
