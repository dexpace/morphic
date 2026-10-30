// Package lowering holds the immutable context every OpenAPI lowering reads.
//
// It is the substrate the lowering packages share, not a stage: it lowers and
// reports nothing itself. It owns the answer to "what is being lowered, and
// what does the document say about itself": the parsed document, the source's
// identity, the indexes derived once at entry, and the constructors that stamp
// provenance.
//
// It is a package because every lowering package needs those answers, and only
// one beneath them all serves each: the schema walk may not reach the
// operation walk, which reaches it, and auth and document reach neither
// (micro-compiler-design §5.1).
package lowering
