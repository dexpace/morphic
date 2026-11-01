// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package exectest builds the commands the tests of the module start as
// children, so a child of a coverage build never shares a coverage directory
// with another. It is test infrastructure outside the compile pipeline, and
// reaches renameio for the whole-file copy and nothing of the module.
package exectest
