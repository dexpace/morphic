// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package whole reads a compiled document once it is whole, for the steps
// that run after the lowerings: it lists the operations and indexes the
// members, parameters and headers those steps name (Document). It imports ir
// alone, so it holds what the document states and nothing of a source's
// context, a policy or a step's verdict.
package whole
