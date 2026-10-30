// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
)

func TestMajorMinor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3.1", detect.MajorMinor("3.1.0"))
	assert.Equal(t, "3.1", detect.MajorMinor("3.1"))
	assert.Equal(t, "4", detect.MajorMinor("4"))
}
