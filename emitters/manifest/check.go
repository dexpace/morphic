// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	digestPrefix       = "sha256:"
	sha256HexLength    = 64
	maxTemporaryDigits = 19
)

type schemaHeader struct {
	Schema int `json:"schema"`
}

func checkSchema(raw []byte, kind string) error {
	var header schemaHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	if header.Schema > Schema {
		return fmt.Errorf("decode %s: schema %d exceeds supported schema %d: %w",
			kind, header.Schema, Schema, ErrNewerSchema)
	}
	if header.Schema < 1 {
		return fmt.Errorf("decode %s: schema %d must be at least 1", kind, header.Schema)
	}
	return nil
}

func checkManifest(manifest Manifest) error {
	if manifest.Target == "" {
		return errors.New("target is empty")
	}
	if manifest.Options < 1 {
		return fmt.Errorf("options version %d must be at least 1", manifest.Options)
	}
	if err := checkInputs(manifest.Inputs); err != nil {
		return err
	}
	seen := make(map[string]bool, len(manifest.Files))
	for _, entry := range manifest.Files {
		if err := checkFilePath(entry.Path); err != nil {
			return err
		}
		if seen[entry.Path] {
			return fmt.Errorf("duplicate path %q", entry.Path)
		}
		seen[entry.Path] = true
		if err := checkDigest(entry.Path, entry.Digest); err != nil {
			return err
		}
	}
	return nil
}

func checkInputs(inputs Inputs) error {
	if inputs.Doc == "" {
		return errors.New("inputs.doc is empty")
	}
	if inputs.Plan == "" {
		return errors.New("inputs.plan is empty")
	}
	if inputs.Emitter == "" {
		return errors.New("inputs.emitter is empty")
	}
	if inputs.Config == "" {
		return errors.New("inputs.config is empty")
	}
	return nil
}

func checkPending(pending Pending) error {
	if pending.Target == "" {
		return errors.New("target is empty")
	}
	for _, listed := range slices.Sorted(maps.Keys(pending.Known)) {
		if err := checkFilePath(listed); err != nil {
			return err
		}
		digests := pending.Known[listed]
		if len(digests) == 0 {
			return fmt.Errorf("path %q has no digest", listed)
		}
		seenDigests := make(map[string]bool, len(digests))
		for _, digest := range digests {
			if err := checkDigest(listed, digest); err != nil {
				return err
			}
			if seenDigests[digest] {
				return fmt.Errorf("path %q repeats digest %q", listed, digest)
			}
			seenDigests[digest] = true
		}
	}
	seenTemp := make(map[string]bool, len(pending.TemporaryFiles))
	for _, listed := range pending.TemporaryFiles {
		if err := checkLocalPath(listed); err != nil {
			return err
		}
		if seenTemp[listed] {
			return fmt.Errorf("duplicate temporary file %q", listed)
		}
		seenTemp[listed] = true
		if err := checkTemporaryFile(listed, pending.Known); err != nil {
			return err
		}
	}
	return nil
}

func checkFilePath(listed string) error {
	if err := checkLocalPath(listed); err != nil {
		return err
	}
	if isReservedPath(listed) {
		return fmt.Errorf("path %q uses a reserved manifest name", listed)
	}
	return nil
}

func checkLocalPath(listed string) error {
	if strings.ContainsRune(listed, '\\') {
		return fmt.Errorf("path %q contains a backslash", listed)
	}
	if path.Clean(listed) != listed {
		return fmt.Errorf("path %q is not clean", listed)
	}
	if listed == "." {
		return fmt.Errorf("path %q does not name a file", listed)
	}
	if !filepath.IsLocal(filepath.FromSlash(listed)) {
		return fmt.Errorf("path %q is not local", listed)
	}
	return nil
}

func isReservedPath(listed string) bool {
	first, _, _ := strings.Cut(listed, "/")
	return first == FileName || first == PendingName || first == PendingTemporaryName
}

func checkDigest(listed, digest string) error {
	hexPart, ok := strings.CutPrefix(digest, digestPrefix)
	if !ok || len(hexPart) != sha256HexLength {
		return fmt.Errorf("path %q has invalid digest %q", listed, digest)
	}
	for i := range len(hexPart) {
		ch := hexPart[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return fmt.Errorf("path %q has invalid digest %q", listed, digest)
		}
	}
	return nil
}

func checkTemporaryFile(listed string, known map[string][]string) error {
	dir, base := path.Split(listed)
	if !strings.HasPrefix(base, ".") {
		return fmt.Errorf("temporary file %q must start with a dot in its base name", listed)
	}
	rest := base[1:]
	for digits := 1; digits <= maxTemporaryDigits && digits < len(rest); digits++ {
		digitPos := len(rest) - digits
		if rest[digitPos] < '0' || rest[digitPos] > '9' {
			break
		}
		targetName := rest[:digitPos]
		if dir == "" && targetName == FileName {
			return nil
		}
		if _, ok := known[dir+targetName]; ok {
			return nil
		}
	}
	return fmt.Errorf("temporary file %q matches no known path or %s", listed, FileName)
}
