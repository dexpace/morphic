// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrNotPortable is wrapped by every error [Portable] returns when a path
// segment violates a cross-filesystem portability rule.
var ErrNotPortable = errors.New("path segment is not portable")

const (
	maxSegmentBytes            = 255
	maxMarkRun                 = 30
	windowsForbiddenCharacters = "<>:\"/\\|?*"
)

// Portable reports whether segment can be written on every target file system,
// returning nil if so or an error wrapping [ErrNotPortable] naming the first
// rule broken (docs/emitters/emit/rules.md).
//
// Refused segments are empty, invalid UTF-8, longer than 255 bytes, hold a
// control character below U+0020, a Windows-forbidden character, a code point
// the build's Unicode tables leave unassigned or more than 30 combining marks
// in a row, end in a dot or a space, or name a Windows device before the first
// dot.
func Portable(segment string) error {
	if segment == "" {
		return fmt.Errorf("segment %.64q is empty: %w", segment, ErrNotPortable)
	}
	if !utf8.ValidString(segment) {
		return fmt.Errorf("segment %.64q is not UTF-8: %w", segment, ErrNotPortable)
	}
	if len(segment) > maxSegmentBytes {
		return fmt.Errorf("segment %.64q is longer than %d bytes: %w", segment, maxSegmentBytes, ErrNotPortable)
	}
	for _, r := range segment {
		if r < 0x20 || strings.ContainsRune(windowsForbiddenCharacters, r) {
			return fmt.Errorf("segment %.64q holds %q, which Windows refuses in a name: %w", segment, r, ErrNotPortable)
		}
		if unicode.Is(unicode.Cn, r) {
			return fmt.Errorf("segment %.64q holds %U, which Unicode leaves unassigned: %w", segment, r, ErrNotPortable)
		}
	}
	if longestMarkRun(segment) > maxMarkRun {
		return fmt.Errorf("segment %.64q holds more than %d combining marks in a row: %w",
			segment, maxMarkRun, ErrNotPortable)
	}
	last := segment[len(segment)-1]
	if last == '.' || last == ' ' {
		return fmt.Errorf("segment %.64q ends in a dot or a space: %w", segment, ErrNotPortable)
	}
	if device, isDevice := windowsDeviceName(segment); isDevice {
		return fmt.Errorf("segment %.64q is the Windows device name %q: %w", segment, device, ErrNotPortable)
	}
	return nil
}

// windowsDeviceName returns the lowercased Windows reserved device name that
// segment names before its first dot (with trailing spaces stripped), if any.
func windowsDeviceName(segment string) (string, bool) {
	const (
		minDeviceBytes = 3
		maxDeviceBytes = 7
	)
	stem, _, _ := strings.Cut(segment, ".")
	stem = strings.TrimRight(stem, " ")
	if len(stem) < minDeviceBytes || len(stem) > maxDeviceBytes {
		return "", false
	}

	var loweredBytes [maxDeviceBytes]byte
	for i := range len(stem) {
		character := stem[i]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		loweredBytes[i] = character
	}
	lowered := string(loweredBytes[:len(stem)])

	switch lowered {
	case "con", "prn", "aux", "nul", "conin$", "conout$":
		return lowered, true
	}
	if !strings.HasPrefix(lowered, "com") && !strings.HasPrefix(lowered, "lpt") {
		return "", false
	}
	suffix := lowered[3:]
	r, size := utf8.DecodeRuneInString(suffix)
	if size == 0 || size != len(suffix) {
		return "", false
	}
	if (r >= '0' && r <= '9') || r == '\u00b9' || r == '\u00b2' || r == '\u00b3' {
		return lowered, true
	}
	return "", false
}
