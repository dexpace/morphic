// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// Availability stores the versioning timeline of an entity
// (docs/ir/availability.md), following the TypeSpec model; the version-slice
// pass produces a concrete snapshot document per version. Formats without
// versioning leave this nil.
type Availability struct {
	// Added lists the version labels at which the entity was added, ordered;
	// multiple entries with Removed support add/remove/re-add cycles.
	Added []string `json:"added,omitempty"`
	// Removed lists the version labels at which the entity was removed.
	Removed []string `json:"removed,omitempty"`
	// Deprecated is the version at which the entity was deprecated.
	Deprecated string `json:"deprecated,omitempty"`
	// RenamedFrom records prior names by version.
	RenamedFrom []VersionedName `json:"renamedFrom,omitempty"`
	// TypeChangedFrom records prior property/return types by version (TypeSpec
	// @typeChangedFrom).
	TypeChangedFrom []VersionedType `json:"typeChangedFrom,omitempty"`
	// RequiredChanged records optionality flips by version (TypeSpec
	// @madeOptional/@madeRequired); the slice pass reconstructs Required for older
	// snapshots from it.
	RequiredChanged []VersionedBool `json:"requiredChanged,omitempty"`
}

// VersionedName is a prior name effective at a version
// (docs/ir/availability.md).
type VersionedName struct {
	// Version is the version label.
	Version string `json:"version,omitempty"`
	// Name is the name effective at that version.
	Name string `json:"name,omitempty"`
}

// VersionedType is a prior type effective at a version
// (docs/ir/availability.md).
type VersionedType struct {
	// Version is the version label.
	Version string `json:"version,omitempty"`
	// Type is the type effective at that version.
	Type TypeRef `json:"type"`
}

// VersionedBool records the prior required state at a version
// (docs/ir/availability.md).
type VersionedBool struct {
	// Version is the version label.
	Version string `json:"version,omitempty"`
	// WasRequired is the Required state effective at that version.
	WasRequired bool `json:"wasRequired"`
}
