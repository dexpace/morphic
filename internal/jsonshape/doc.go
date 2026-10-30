// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package jsonshape describes the JSON shape of the types one Go type reaches,
// as text for the IRVersion pin and as data for the census and the tests that
// read the IR's types.
//
// A description records what encoding/json/v2 reads, never what it does with
// it: tagged members and embeddings, each one's type, the methods the encoder
// calls in place of reading fields, a type's constants, a sealed sum's
// variants, and any name that gives a value its meaning. Restating no encoder
// rule keeps the two from drifting apart. A codec method's body is the one
// input it does not read.
package jsonshape
