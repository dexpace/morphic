// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package detect recognizes an OpenAPI or Swagger document from its bytes: the
// discriminating key it writes and the version beside it, read off the parse
// the compile goes on to lower.
//
// The root mapping is read through nodeview, as the parser library reads it, so
// the version that routes a source is the version its compile reads
// (docs/compilers/openapi.md#yaml-decoding). Only bytes that will not parse are
// read another way: a scan for a version written where a document declares
// one, which decides whether the failure is this compiler's to report.
package detect
