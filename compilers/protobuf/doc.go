// Package protobuf lowers Protocol Buffers .proto schemas (proto2, proto3, and
// editions, including gRPC service definitions) into the Morphic IR. It
// implements compilers.Compiler. Parsing is delegated to
// github.com/bufbuild/protocompile, a pure-Go .proto compiler that produces
// fully linked descriptors with resolved edition features and bundled
// well-known-type imports. This package owns identity (fully-qualified-name
// derived IDs), the mapping of protobuf's presence/oneof/encoding semantics onto
// the IR, and lossless preservation of constructs the IR does not model
// structurally (reserved ranges, custom options, file options).
package protobuf
