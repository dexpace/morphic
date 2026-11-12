// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package oas stands for the OpenAPI compiler's rule table, which may read a
// version: its reading of one draws nothing here.
package oas

// ParseMinor stands for the table's reading of a version.
func ParseMinor(version string) (int, bool) { return len(version), version != "" }

// Covers asks it, as the table does.
func Covers(version string) bool {
	_, ok := ParseMinor(version)
	return ok
}
