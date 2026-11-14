// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// coreWithoutNode is a model whose core holds no node.
type coreWithoutNode struct{}

// GetCoreAny returns a core that is no node holder.
func (coreWithoutNode) GetCoreAny() any { return 0 }

// TestWrittenAs_ReadsNoNodeOffAModelHoldingNone pins writtenAs on a model that
// holds no core, and on one whose core holds no node: neither writes a mapping.
func TestWrittenAs_ReadsNoNodeOffAModelHoldingNone(t *testing.T) {
	t.Parallel()
	assert.Nil(t, writtenAs(0))
	assert.Nil(t, writtenAs(coreWithoutNode{}))
}
