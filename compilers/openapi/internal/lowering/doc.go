// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package lowering holds the context the OpenAPI schema lowering reads.
//
// It is the substrate the lowering packages share, not a stage: it lowers and
// reports nothing. It owns the answer to "which document is being read": the
// site a lowering places and names what it reads by, the bindings and the rows
// each schema is read with, and the indexes derived once at entry. What only
// the service lowerings read is package service's.
//
// It is a package because only one beneath every lowering package serves
// those answers: the schema walk may not reach the operation walk, which
// reaches it (micro-compiler-design §5.1).
package lowering
