// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package service holds the context the service lowerings read: the
// operation walk, the security schemes and requirements, the servers and the
// document's own metadata.
//
// It is the schema lowering's context (lowering.Ctx) and what only those
// lowerings read beside it: the parsed document, the source's identity, the
// fields the load phase modelled beside path items' $refs and the media types
// it resolved, how operations group, which media types stream or are binary,
// and the budget a $ref's copies are charged against. The schema walk reads
// none of it, so it reaches lowering and never this package.
package service
