// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsOffset_ReadsTheGrammar pins the offset grammar on its own terms. A
// time format reaches it only through parsesRFC3339, whose time.Parse also
// refuses a non-digit offset, so no format test fails when isOffset reads a
// byte that is no digit as a number.
func TestIsOffset_ReadsTheGrammar(t *testing.T) {
	t.Parallel()
	for offset, want := range map[string]bool{
		"Z": true, "z": true, "+00:00": true, "-23:59": true,
		"+24:00": false, "+05:60": false,
		"+0a:30": false, "+a0:30": false, "+05:3a": false, "+05:a0": false,
		"*05:30": false, "+05-30": false, "+0530": false, "05:30": false, "": false,
	} {
		assert.Equal(t, want, isOffset(offset), "%q", offset)
	}
}
