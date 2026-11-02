// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package replacement fills what a deprecation names as its replacement
// (ir.Deprecation.ReplacedBy) from the vendor extensions a policy maps to
// lowering.TargetDeprecationReplacedBy. It runs once the document is whole,
// since a replacement may be declared after what it replaces, and reads the
// extensions the nodes keep, as every promotion does.
package replacement
