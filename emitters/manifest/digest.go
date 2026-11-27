// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

const utf8BOM = "\xef\xbb\xbf"

// Digest returns "sha256:" followed by the lowercase hex SHA-256 of content
// after stripping at most one leading UTF-8 byte-order mark, converting CRLF
// line endings to LF, and dropping trailing LF bytes
// (docs/emitters/write-integrate.md).
//
// A lone CR is kept as content. Keep regions are not excluded yet; region
// normalization lands with merging (GitHub #2263).
func Digest(content []byte) string {
	trimmed := bytes.TrimPrefix(content, []byte(utf8BOM))
	normalized := bytes.ReplaceAll(trimmed, []byte("\r\n"), []byte("\n"))
	normalized = bytes.TrimRight(normalized, "\n")
	sum := sha256.Sum256(normalized)
	return fmt.Sprintf("%s%x", digestPrefix, sum)
}
