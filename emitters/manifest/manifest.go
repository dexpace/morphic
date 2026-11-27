// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	// Schema is the manifest schema version this build reads and writes.
	Schema = 1
	// FileName is the manifest file kept at a target's output root.
	FileName = "morphic.manifest.json"
	// PendingName is the pending manifest kept beside [FileName] while a write
	// is in flight.
	PendingName = "morphic.manifest.pending.json"
	// PendingTemporaryName is the fixed temporary file through which
	// [PendingName] is replaced atomically.
	PendingTemporaryName = ".morphic.manifest.pending.json.tmp"
)

// ErrNewerSchema is wrapped when decoding a manifest or pending manifest whose
// schema version exceeds [Schema].
var ErrNewerSchema = errors.New("manifest is of a newer schema")

// Manifest records the inputs and files of one completed generation at a
// target's output root (docs/emitters/write-integrate.md).
type Manifest struct {
	Schema  int    `json:"schema"`
	Target  string `json:"target"`
	Options int    `json:"options"`
	Inputs  Inputs `json:"inputs"`
	Files   []File `json:"files,omitempty"`
}

// Inputs records the digests and build identity a generation was produced from.
type Inputs struct {
	Doc     string `json:"doc"`
	Plan    string `json:"plan"`
	Emitter string `json:"emitter"`
	Config  string `json:"config"`
}

// File records one generated file's root-relative path and normalized digest.
type File struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// Decode reads a canonical or unordered JSON manifest, checking [Schema] before
// strict decoding so a newer manifest returns [ErrNewerSchema] even when it
// carries unknown members. Returned Files are sorted by Path.
func Decode(raw []byte) (Manifest, error) {
	if err := checkSchema(raw, "manifest"); err != nil {
		return Manifest{}, err
	}
	var decoded Manifest
	if err := json.Unmarshal(raw, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := checkManifest(decoded); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	sortFiles(decoded.Files)
	return decoded, nil
}

// Encode validates manifest, sorts a copy of its Files by Path without
// modifying the caller's slice, and encodes it as two-space indented
// deterministic JSON with a trailing newline.
func Encode(manifest Manifest) ([]byte, error) {
	if manifest.Schema != Schema {
		return nil, fmt.Errorf("encode manifest: schema %d must be %d", manifest.Schema, Schema)
	}
	if err := checkManifest(manifest); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	canonical := manifest
	if len(manifest.Files) > 0 {
		canonical.Files = slices.Clone(manifest.Files)
		sortFiles(canonical.Files)
	}
	raw, err := json.Marshal(canonical, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

func sortFiles(files []File) {
	slices.SortFunc(files, func(left, right File) int {
		return strings.Compare(left.Path, right.Path)
	})
}
