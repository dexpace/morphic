// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package plan derives the language-neutral decisions every emitter reads
// from an [ir.Document], once for all of them (docs/emitters/plan/overview.md).
// [Build] is pure and deterministic, and a [Plan] encodes as JSON, so one is
// cached, shared between processes and golden-tested with no target in the
// loop.
//
// The plan classifies and each target decides
// (docs/emitters/plan/plan-or-refine.md): it holds no casing, no reserved
// word, no target type and no runtime policy, and copies no fact the document
// states outright. A key naming a node is one of the document's stable IDs.
//
// It reports its own findings under emitters/plan/ codes, once for every
// target.
package plan
