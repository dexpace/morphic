// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest_test

import (
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
	base := manifest.Digest([]byte("a\nb"))
	rows := []struct {
		name    string
		content string
	}{
		{name: "added leading space on second line", content: "a\n b"},
		{name: "lone CR", content: "a\rb"},
		{name: "double BOM", content: "\ufeff\ufeffa\nb"},
		{name: "interior blank line", content: "a\n\nb"},
		{name: "leading space", content: " a\nb"},
		{name: "trailing space", content: "a\nb "},
		{name: "CR before CRLF", content: "a\r\r\nb"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.NotEqual(t, base, manifest.Digest([]byte(row.content)), "content %q", row.content)
		})
	}
}
