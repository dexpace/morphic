// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package operation lowers what a document says an API does: its path items,
// webhooks and callbacks, the parameters merged onto each operation, and the
// content of every request body, response and header.
//
// It sits above the schema walk, which it reaches for every type position, and
// above auth, for the requirements an operation names. It reaches nothing above
// itself: the compiler assembles a Document from what LowerService returns.
//
// The recursion is callbacks, each a path item holding operations of its own,
// which is why lowerOperation, lowerCallbacks, lowerCallbackOps and
// mountPathItem cannot be separated. internal/archtest pins that set.
package operation
