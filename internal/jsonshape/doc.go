// Package jsonshape describes the JSON shape of the types one Go type reaches,
// for a test that pins a schema to the version it is published under.
//
// A description records what encoding/json/v2 reads, never what it does with
// it: each struct's tagged members and embeddings, the type each is declared
// with, the methods the encoder calls in place of reading fields, the
// constants a type declares, and a sealed sum's variants. Reading the inputs
// rather than restating the encoder's rules keeps the two from drifting apart.
// A codec method's body is the one input it does not read.
package jsonshape
