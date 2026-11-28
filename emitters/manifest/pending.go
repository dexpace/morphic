// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
)

// Pending records the known file digests and temporary file paths of an
// in-flight generation so an interrupted write heals on the next run
// (docs/emitters/write-integrate.md).
type Pending struct {
	// Schema is the schema the manifest will state.
	Schema int `json:"schema"`
	// Target is the target the manifest will state.
	Target string `json:"target"`
	// Known holds, for each path, every digest known to be the generation's.
	Known map[string][]string `json:"known,omitempty"`
	// TemporaryFiles names each temporary file the plan's writes make, the
	// manifest's among them, with those any earlier pending manifest records.
	TemporaryFiles []string `json:"temporaryFiles,omitempty"`
}

// DecodePending reads a JSON pending manifest, checking [Schema] before strict
// decoding so a newer pending manifest returns [ErrNewerSchema] even with
// unknown members. Returned digests and TemporaryFiles are sorted.
func DecodePending(raw []byte) (Pending, error) {
	if err := checkSchema(raw, "pending manifest"); err != nil {
		return Pending{}, err
	}
	var decoded Pending
	if err := json.Unmarshal(raw, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return Pending{}, fmt.Errorf("decode pending manifest: %w", err)
	}
	if err := checkPending(decoded); err != nil {
		return Pending{}, fmt.Errorf("decode pending manifest: %w", err)
	}
	for _, digests := range decoded.Known {
		slices.Sort(digests)
	}
	slices.Sort(decoded.TemporaryFiles)
	return decoded, nil
}

// EncodePending validates pending, sorts copies of its Known digest slices and
// TemporaryFiles without modifying the caller's map or slices, and encodes it
// as two-space indented deterministic JSON with a trailing newline.
func EncodePending(pending Pending) ([]byte, error) {
	if pending.Schema != Schema {
		return nil, fmt.Errorf("encode pending manifest: schema %d must be %d", pending.Schema, Schema)
	}
	if err := checkPending(pending); err != nil {
		return nil, fmt.Errorf("encode pending manifest: %w", err)
	}
	canonical := Pending{
		Schema: pending.Schema,
		Target: pending.Target,
	}
	if len(pending.Known) > 0 {
		canonical.Known = make(map[string][]string, len(pending.Known))
		for _, listed := range slices.Sorted(maps.Keys(pending.Known)) {
			digests := slices.Clone(pending.Known[listed])
			slices.Sort(digests)
			canonical.Known[listed] = digests
		}
	}
	if len(pending.TemporaryFiles) > 0 {
		canonical.TemporaryFiles = slices.Clone(pending.TemporaryFiles)
		slices.Sort(canonical.TemporaryFiles)
	}
	raw, err := json.Marshal(canonical, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("encode pending manifest: %w", err)
	}
	return append(raw, '\n'), nil
}
