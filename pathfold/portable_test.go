// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/pathfold"
)

func TestPortable_RefusesWhatSomeFileSystemRefuses(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name       string
		segment    string
		wantPhrase string
	}{
		{name: "empty", segment: "", wantPhrase: "is empty"},
		{name: "invalid UTF-8 single byte", segment: "\xff", wantPhrase: "is not UTF-8"},
		{name: "invalid UTF-8 truncated sequence", segment: "a\xc3", wantPhrase: "is not UTF-8"},
		{name: "256 ASCII bytes", segment: strings.Repeat("a", 256), wantPhrase: "is longer than 255 bytes"},
		{name: "86 three-byte runes (258 bytes)", segment: strings.Repeat("日", 86), wantPhrase: "is longer than 255 bytes"},
		{name: "less than", segment: "a<b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '<')},
		{name: "greater than", segment: "a>b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '>')},
		{name: "colon", segment: "a:b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", ':')},
		{name: "double quote", segment: `a"b`, wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '"')},
		{name: "slash", segment: "a/b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '/')},
		{name: "backslash", segment: `a\b`, wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '\\')},
		{name: "pipe", segment: "a|b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '|')},
		{name: "question mark", segment: "a?b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '?')},
		{name: "asterisk", segment: "a*b", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '*')},
		{name: "NUL byte", segment: "a\x00", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '\x00')},
		{name: "control character U+001F", segment: "a\x1f", wantPhrase: fmt.Sprintf("holds %q, which Windows refuses in a name", '\x1f')},
		{name: "trailing dot", segment: "a.", wantPhrase: "ends in a dot or a space"},
		{name: "trailing space", segment: "a ", wantPhrase: "ends in a dot or a space"},
		{name: "single dot", segment: ".", wantPhrase: "ends in a dot or a space"},
		{name: "double dot", segment: "..", wantPhrase: "ends in a dot or a space"},
		{name: "device with trailing dot", segment: "con.", wantPhrase: "ends in a dot or a space"},
		{name: "device with trailing space", segment: "con ", wantPhrase: "ends in a dot or a space"},
		{name: "con", segment: "con", wantPhrase: `is the Windows device name "con"`},
		{name: "CON", segment: "CON", wantPhrase: `is the Windows device name "con"`},
		{name: "prn", segment: "prn", wantPhrase: `is the Windows device name "prn"`},
		{name: "PRN", segment: "PRN", wantPhrase: `is the Windows device name "prn"`},
		{name: "aux", segment: "aux", wantPhrase: `is the Windows device name "aux"`},
		{name: "Aux", segment: "Aux", wantPhrase: `is the Windows device name "aux"`},
		{name: "nul", segment: "nul", wantPhrase: `is the Windows device name "nul"`},
		{name: "NUL", segment: "NUL", wantPhrase: `is the Windows device name "nul"`},
		{name: "conin$", segment: "conin$", wantPhrase: `is the Windows device name "conin$"`},
		{name: "CONIN$", segment: "CONIN$", wantPhrase: `is the Windows device name "conin$"`},
		{name: "conout$", segment: "conout$", wantPhrase: `is the Windows device name "conout$"`},
		{name: "CONOUT$", segment: "CONOUT$", wantPhrase: `is the Windows device name "conout$"`},
		{name: "com0", segment: "com0", wantPhrase: `is the Windows device name "com0"`},
		{name: "COM0", segment: "COM0", wantPhrase: `is the Windows device name "com0"`},
		{name: "com1", segment: "com1", wantPhrase: `is the Windows device name "com1"`},
		{name: "COM1", segment: "COM1", wantPhrase: `is the Windows device name "com1"`},
		{name: "com9", segment: "com9", wantPhrase: `is the Windows device name "com9"`},
		{name: "COM9", segment: "COM9", wantPhrase: `is the Windows device name "com9"`},
		{name: "lpt0", segment: "lpt0", wantPhrase: `is the Windows device name "lpt0"`},
		{name: "LPT0", segment: "LPT0", wantPhrase: `is the Windows device name "lpt0"`},
		{name: "lpt9", segment: "lpt9", wantPhrase: `is the Windows device name "lpt9"`},
		{name: "LPT9", segment: "LPT9", wantPhrase: `is the Windows device name "lpt9"`},
		{name: "com superscript 1", segment: "com\u00b9", wantPhrase: "is the Windows device name \"com\u00b9\""},
		{name: "COM superscript 1", segment: "COM\u00b9", wantPhrase: "is the Windows device name \"com\u00b9\""},
		{name: "com superscript 2", segment: "com\u00b2", wantPhrase: "is the Windows device name \"com\u00b2\""},
		{name: "COM superscript 2", segment: "COM\u00b2", wantPhrase: "is the Windows device name \"com\u00b2\""},
		{name: "lpt superscript 3", segment: "lpt\u00b3", wantPhrase: "is the Windows device name \"lpt\u00b3\""},
		{name: "LPT superscript 3", segment: "LPT\u00b3", wantPhrase: "is the Windows device name \"lpt\u00b3\""},
		{name: "con before extension", segment: "con.txt", wantPhrase: `is the Windows device name "con"`},
		{name: "aux before version extension", segment: "aux.v1", wantPhrase: `is the Windows device name "aux"`},
		{name: "AUX before version extension", segment: "AUX.V1", wantPhrase: `is the Windows device name "aux"`},
		{name: "aux before two dots", segment: "aux.v1.go", wantPhrase: `is the Windows device name "aux"`},
		{name: "nul before space and dot", segment: "nul .txt", wantPhrase: `is the Windows device name "nul"`},
		{name: "aux before space and two dots", segment: "aux .tar.gz", wantPhrase: `is the Windows device name "aux"`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			err := pathfold.Portable(row.segment)
			assert.ErrorIs(t, err, pathfold.ErrNotPortable)
			assert.ErrorContains(t, err, row.wantPhrase)
		})
	}
}

func TestPortable_Admits(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name    string
		segment string
	}{
		{name: "255 ASCII bytes", segment: strings.Repeat("a", 255)},
		{name: "85 three-byte runes (255 bytes)", segment: strings.Repeat("日", 85)},
		{name: "internal space", segment: "a b"},
		{name: "console", segment: "console"},
		{name: "com without digit", segment: "com"},
		{name: "com10", segment: "com10"},
		{name: "coma", segment: "coma"},
		{name: "com superscript 4", segment: "com\u2074"},
		{name: "conin without dollar", segment: "conin"},
		{name: "lpt without digit", segment: "lpt"},
		{name: "leading space before con", segment: " con"},
		{name: "nul with underscore", segment: "nul_"},
		{name: "dotfile", segment: ".gitignore"},
		{name: "pending manifest temporary name", segment: ".morphic.manifest.pending.json.tmp"},
		{name: "leading double dot", segment: "..a"},
		{name: "non-ASCII letter", segment: "\u00e9"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, pathfold.Portable(row.segment))
		})
	}
}
