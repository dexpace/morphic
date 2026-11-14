// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestCheck_ABasedIntegerIsTheCoreSchemas holds a count written with a base
// prefix to YAML 1.2's core schema: 0o and 0x before unsigned digits of the
// base are an integer, and any other spelling is text.
func TestCheck_ABasedIntegerIsTheCoreSchemas(t *testing.T) {
	verify(t, []scenario{
		{"octal", "3.1.0", oas.Schema, `{maxLength: 0o17}`, nil},
		{"octal with no digit", "3.1.0", oas.Schema, `{maxLength: 0o}`, []finding{at(mismatch, "/maxLength")}},
		{"octal with a separator", "3.1.0", oas.Schema, `{maxLength: 0o1_7}`, []finding{at(mismatch, "/maxLength")}},
		{"hex with a sign after its prefix", "3.1.0", oas.Schema, `{maxLength: 0x-1}`, []finding{at(mismatch, "/maxLength")}},
		{"octal past its digits", "3.1.0", oas.Schema, `{maxLength: 0o19}`, []finding{at(mismatch, "/maxLength")}},
	})
}
