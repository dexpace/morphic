// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package naming names every node the OpenAPI compiler builds as a function
// of the documents and the node's coordinate (GitHub #981), so which
// lowering reaches a coordinate first cannot change what it is called.
//
// A name is an owner's, then the structure beneath it. The owner is the
// nearest object holding a schema as the grammar reads it (bind.Table.Kinds):
// a component schema, request body, response, parameter or header, or a
// document whose root it is. Each keyword beneath composes the name of the
// position it leads to; a branch holding a $ref takes the name of the
// position it lands on.
package naming
