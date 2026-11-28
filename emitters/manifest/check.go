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
	"unicode/utf8"

	"github.com/dexpace/morphic/pathfold"
)

const (
	sha256HexLength = 64
	// maxTemporaryDigits mirrors docs/emitters/write-integrate.md: a temporary
	// file's name is a dot, its file's name and a number of up to 19 digits.
	maxTemporaryDigits = 19
)

// maxReservedSpellingBytes bounds a spelling of a reserved name: a fold holds
// no fewer characters than what it folds, and a character is at most
// utf8.UTFMax bytes.
const maxReservedSpellingBytes = utf8.UTFMax *
	max(len(FileName), len(PendingName), len(PendingTemporaryName))

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
	for _, listedPath := range slices.Sorted(maps.Keys(known)) {
		if err := checkFilePath(listedPath); err != nil {
			return err
		}
		digests := known[listedPath]
		if len(digests) == 0 {
			return fmt.Errorf("path %.64q has no digest", listedPath)
		}
		seenDigests := make(map[string]bool, len(digests))
		for _, digest := range digests {
			if err := checkDigest(listedPath, digest); err != nil {
				return err
			}
			if seenDigests[digest] {
				return fmt.Errorf("path %.64q repeats digest %.64q", listedPath, digest)
			}
			seenDigests[digest] = true
		}
	}
	return nil
}

// checkPendingTemporaryFiles admits a temporary file that is also a known path: a
// later generation can hold a name an earlier pending manifest recorded
// (docs/emitters/write-integrate.md, apply step 2). How the planner treats such
// a file is the planner's to decide (GitHub #2246).
func checkPendingTemporaryFiles(temporaryFiles []string, known map[string][]string) error {
	seenTemporaryFiles := make(map[string]bool, len(temporaryFiles))
	for _, temporaryFile := range temporaryFiles {
		// Kept for its clearer message: checkTemporaryFile admits only a name beside a held path.
		if err := checkLocalPath(temporaryFile); err != nil {
			return err
		}
		if seenTemporaryFiles[temporaryFile] {
			return fmt.Errorf("duplicate temporary file %.64q", temporaryFile)
		}
		seenTemporaryFiles[temporaryFile] = true
		if err := checkTemporaryFile(temporaryFile, known); err != nil {
			return err
		}
	}
	return nil
}

func checkFilePath(listedPath string) error {
	if err := checkLocalPath(listedPath); err != nil {
		return err
	}
	if isReservedPath(listedPath) {
		return fmt.Errorf("path %.64q uses a reserved manifest name", listedPath)
	}
	return nil
}

func checkLocalPath(listedPath string) error {
	if strings.ContainsRune(listedPath, '\\') {
		return fmt.Errorf("path %.64q contains a backslash", listedPath)
	}
	if path.Clean(listedPath) != listedPath {
		return fmt.Errorf("path %.64q is not clean", listedPath)
	}
	if listedPath == "." {
		return fmt.Errorf("path %.64q does not name a file", listedPath)
	}
	// filepath.IsLocal reads the host's path syntax, so a Windows host refuses
	// paths another admits; generation is not built for Windows
	// (docs/emitters/write-integrate.md#where-generation-runs).
	if !filepath.IsLocal(filepath.FromSlash(listedPath)) {
		return fmt.Errorf("path %.64q is not local", listedPath)
	}
	return nil
}

func isReservedPath(listedPath string) bool {
	first, _, _ := strings.Cut(listedPath, "/")
	if len(first) > maxReservedSpellingBytes {
		return false
	}
	// The names are their own folds (TestFold_LeavesEachReservedNameAsWritten),
	// so only the listed segment is folded.
	folded := pathfold.Fold(first)
	return folded == FileName || folded == PendingName || folded == PendingTemporaryName
}

func checkDigest(listedPath, digest string) error {
	hexDigits, ok := strings.CutPrefix(digest, digestPrefix)
	if !ok || len(hexDigits) != sha256HexLength {
		return fmt.Errorf("path %.64q has invalid digest %.64q", listedPath, digest)
	}
	for i := range len(hexDigits) {
		character := hexDigits[i]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("path %.64q has invalid digest %.64q", listedPath, digest)
		}
	}
	return nil
}

func checkTemporaryFile(temporaryFile string, known map[string][]string) error {
	dir, base := path.Split(temporaryFile)
	if !strings.HasPrefix(base, ".") {
		return fmt.Errorf("temporary file %.64q must start with a dot in its base name", temporaryFile)
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
	return fmt.Errorf("temporary file %.64q matches no known path or %s", temporaryFile, FileName)
}
