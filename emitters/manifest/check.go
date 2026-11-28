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
	sha256HexLength    = 64
	maxTemporaryDigits = 19
)

// schemaHeader reads the schema alone; a pointer tells an absent or null
// schema from a present one.
type schemaHeader struct {
	Schema *int `json:"schema"`
}

func checkSchema(raw []byte, kind string) error {
	var header schemaHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	if header.Schema == nil {
		return fmt.Errorf("decode %s: schema is missing", kind)
	}
	schema := *header.Schema
	if schema > Schema {
		return fmt.Errorf("decode %s: schema %d exceeds supported schema %d: %w",
			kind, schema, Schema, ErrNewerSchema)
	}
	if schema < 1 {
		return fmt.Errorf("decode %s: schema %d must be at least 1", kind, schema)
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
			return fmt.Errorf("duplicate path %.64q", entry.Path)
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
	if err := checkPendingKnown(pending.Known); err != nil {
		return err
	}
	return checkPendingTemporaryFiles(pending.TemporaryFiles, pending.Known)
}

func checkPendingKnown(known map[string][]string) error {
	for _, listed := range slices.Sorted(maps.Keys(known)) {
		if err := checkFilePath(listed); err != nil {
			return err
		}
		digests := known[listed]
		if len(digests) == 0 {
			return fmt.Errorf("path %.64q has no digest", listed)
		}
		seenDigests := make(map[string]bool, len(digests))
		for _, digest := range digests {
			if err := checkDigest(listed, digest); err != nil {
				return err
			}
			if seenDigests[digest] {
				return fmt.Errorf("path %.64q repeats digest %.64q", listed, digest)
			}
			seenDigests[digest] = true
		}
	}
	return nil
}

func checkPendingTemporaryFiles(temporaryFiles []string, known map[string][]string) error {
	seenTemporaryFiles := make(map[string]bool, len(temporaryFiles))
	for _, listed := range temporaryFiles {
		// Kept for its clearer message: checkTemporaryFile admits only a name beside a held path.
		if err := checkLocalPath(listed); err != nil {
			return err
		}
		if seenTemporaryFiles[listed] {
			return fmt.Errorf("duplicate temporary file %.64q", listed)
		}
		seenTemporaryFiles[listed] = true
		if err := checkTemporaryFile(listed, known); err != nil {
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
		return fmt.Errorf("path %.64q uses a reserved manifest name", listed)
	}
	return nil
}

func checkLocalPath(listed string) error {
	if strings.ContainsRune(listed, '\\') {
		return fmt.Errorf("path %.64q contains a backslash", listed)
	}
	if path.Clean(listed) != listed {
		return fmt.Errorf("path %.64q is not clean", listed)
	}
	if listed == "." {
		return fmt.Errorf("path %.64q does not name a file", listed)
	}
	if !filepath.IsLocal(filepath.FromSlash(listed)) {
		return fmt.Errorf("path %.64q is not local", listed)
	}
	return nil
}

func isReservedPath(listed string) bool {
	first, _, _ := strings.Cut(listed, "/")
	return first == FileName || first == PendingName || first == PendingTemporaryName
}

func checkDigest(listed, digest string) error {
	hexDigits, ok := strings.CutPrefix(digest, digestPrefix)
	if !ok || len(hexDigits) != sha256HexLength {
		return fmt.Errorf("path %.64q has invalid digest %.64q", listed, digest)
	}
	for i := range len(hexDigits) {
		character := hexDigits[i]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("path %.64q has invalid digest %.64q", listed, digest)
		}
	}
	return nil
}

func checkTemporaryFile(listed string, known map[string][]string) error {
	dir, base := path.Split(listed)
	if !strings.HasPrefix(base, ".") {
		return fmt.Errorf("temporary file %.64q must start with a dot in its base name", listed)
	}
	rest := base[1:]
	for digits := 1; digits <= maxTemporaryDigits && digits < len(rest); digits++ {
		digitStart := len(rest) - digits
		if rest[digitStart] < '0' || rest[digitStart] > '9' {
			break
		}
		targetName := rest[:digitStart]
		if dir == "" && targetName == FileName {
			return nil
		}
		if _, ok := known[dir+targetName]; ok {
			return nil
		}
	}
	return fmt.Errorf("temporary file %.64q matches no known path or %s", listed, FileName)
}
