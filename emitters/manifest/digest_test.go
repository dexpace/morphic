// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/emitters/manifest"
)

func TestDigest_IsBlindToWhatACheckoutChanges(t *testing.T) {
	t.Parallel()
	const (
		wantLines = "sha256:7e18f737311b2dc3b2f269dd78396b0351f14fb66efa879f768cb23181883c78"
		wantEmpty = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	)

	rows := []struct {
		name    string
		content string
		want    string
	}{
		{name: "base lines", content: "a\nb", want: wantLines},
		{name: "trailing LF", content: "a\nb\n", want: wantLines},
		{name: "CRLF with trailing CRLF", content: "a\r\nb\r\n", want: wantLines},
		{name: "leading BOM", content: "\ufeffa\nb", want: wantLines},
		{name: "multiple trailing LF", content: "a\nb\n\n", want: wantLines},
		{name: "CRLF without trailing newline", content: "a\r\nb", want: wantLines},
		{name: "BOM and multiple trailing CRLF", content: "\ufeffa\r\nb\r\n\r\n", want: wantLines},
		{name: "empty", content: "", want: wantEmpty},
		{name: "single LF", content: "\n", want: wantEmpty},
		{name: "single CRLF", content: "\r\n", want: wantEmpty},
		{name: "BOM only", content: "\ufeff", want: wantEmpty},
		{name: "BOM and LF", content: "\ufeff\n", want: wantEmpty},
		{name: "CR before CRLF", content: "a\r\r\nb", want: wantLines},
		{name: "CRs before CRLF and a trailing CR before CRLF", content: "a\r\r\r\nb\r\r\n", want: wantLines},
		{name: "LF and a trailing CR before CRLF", content: "a\nb\r\r\n", want: wantLines},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, row.want, manifest.Digest([]byte(row.content)), "content %q", row.content)
		})
	}
}

func TestDigest_SeesWhatAnEditChanges(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		content  string
		unedited string
	}{
		{name: "added leading space on second line", content: "a\n b"},
		{name: "lone CR", content: "a\rb"},
		{name: "double BOM", content: "\ufeff\ufeffa\nb"},
		{name: "interior blank line", content: "a\n\nb"},
		{name: "leading space", content: " a\nb"},
		{name: "trailing space", content: "a\nb "},
		{name: "trailing CR", content: "a\nb\r"},
		{name: "trailing CR alone", content: "a\r", unedited: "a"},
		{name: "CR after LF", content: "a\n\rb"},
		{name: "leading LF", content: "\na\nb"},
		{name: "space before LF", content: "a \nb"},
		{name: "tab before LF", content: "a\t\nb"},
		{name: "trailing BOM", content: "a\nb\ufeff"},
		{name: "BOM inside content", content: "a\n\ufeffb"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			unedited := row.unedited
			if unedited == "" {
				unedited = "a\nb"
			}
			assert.NotEqual(t, manifest.Digest([]byte(unedited)), manifest.Digest([]byte(row.content)),
				"content %q against %q", row.content, unedited)
		})
	}
}

// TestDigest_IsBlindToEveryCheckoutOfShortContent holds every string of up to
// seven bytes of a, CR and LF to the SHA-256 of its canonical spelling, to
// digesting as that spelling does, and as itself with a CR inserted before, or
// removed from before, any LF.
func TestDigest_IsBlindToEveryCheckoutOfShortContent(t *testing.T) {
	t.Parallel()
	const maxLength = 7
	alphabet := []byte("a\r\n")
	carriageReturnsBeforeLineFeed := regexp.MustCompile("\r+\n")
	canonical := func(content string) string {
		return strings.TrimRight(carriageReturnsBeforeLineFeed.ReplaceAllString(content, "\n"), "\n")
	}

	checked := 0
	for length := 0; length <= maxLength; length++ {
		combinations := 1
		for range length {
			combinations *= len(alphabet)
		}
		for combination := range combinations {
			content := make([]byte, length)
			for position, remaining := 0, combination; position < length; position++ {
				content[position] = alphabet[remaining%len(alphabet)]
				remaining /= len(alphabet)
			}
			checked++
			checkCheckoutBlindness(t, string(content), canonical(string(content)))
		}
	}
	assert.Equal(t, 3280, checked, "strings of up to %d bytes of a, CR and LF", maxLength)
}

func checkCheckoutBlindness(t *testing.T, content, canonical string) {
	t.Helper()
	want := manifest.Digest([]byte(content))
	canonicalSum := sha256.Sum256([]byte(canonical))
	assert.Equal(t, "sha256:"+hex.EncodeToString(canonicalSum[:]), want, "%q digested as %q", content, canonical)
	assert.Equal(t, want, manifest.Digest([]byte(canonical)), "%q against its canonical spelling %q", content, canonical)
	for position := range len(content) {
		if content[position] != '\n' {
			continue
		}
		inserted := content[:position] + "\r" + content[position:]
		assert.Equal(t, want, manifest.Digest([]byte(inserted)), "%q against %q", content, inserted)
		if position > 0 && content[position-1] == '\r' {
			removed := content[:position-1] + content[position:]
			assert.Equal(t, want, manifest.Digest([]byte(removed)), "%q against %q", content, removed)
		}
	}
}
