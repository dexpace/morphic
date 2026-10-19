// Package jsonshape describes the JSON shape of the types one Go type reaches,
// for a test that pins a schema to its version.
//
// A description records what encoding/json/v2 reads, never what it does with
// it: tagged members and embeddings, each one's type, and the methods the
// encoder calls in place of reading fields. It adds the values a reader meets,
// a type's constants and a sealed sum's variants, and any name that gives a
// value its meaning. Restating no encoder rule keeps the two from drifting
// apart. A codec method's body is the one input it does not read.
package jsonshape
