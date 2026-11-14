# Payloads

Part of the [IR design](./README.md). A request, response or message body, one `Content` per media
type.

```go
type Payload struct {
    Contents []Content            // one per media type / message schema — all kept; none for a
                                  // request body declaring no media type, which OpenAPI's
                                  // schemas admit
    Required *bool                // true = the body must be sent, false = it may be omitted;
                                  // nil = the source format does not express body optionality.
                                  // Three states, not two: a format that expresses it treats an
                                  // unstated body as optional, so folding that onto nil would make
                                  // "the format is silent" read as "the document says no".
                                  // Response and message payloads leave it nil — only a request
                                  // body can be omitted — and pass/validate reports one set
                                  // anywhere else (ir/payload-required-outside-request)
    Docs     Docs                 // the body's own documentation, as against any one media
                                  // type's: an OpenAPI Request Body Object's description, a
                                  // TypeSpec @doc on a @body parameter. Omitted when empty,
                                  // unlike a named node's
    Unmodeled  Unmodeled
}

type Content struct {
    MediaType string              // "application/json", "multipart/form-data", "" for non-HTTP
    SchemaFormat string           // schema *language* the type graph was lowered from, media-type
                                  // form (AsyncAPI multiFormatSchema: Avro/Protobuf/RAML/…);
                                  // "" = source-native. The verbatim source schema is preserved in
                                  // Unmodeled (e.g. "asyncapi:schema") for schema-registry workflows
    Type      TypeRef
    Stream    *ContentStream      // the body is a sequence of frames, not one document; nil = one
                                  // document (below)
    Item      *TypeRef            // element shape of a sequential stream declared per media type
                                  // (OpenAPI 3.2 itemSchema for SSE/JSONL/json-seq); nil = none
                                  // declared, Stream saying whether the content streams
    ItemEncoding *PartEncoding    // wire encoding of a sequential stream's item (3.2 itemEncoding),
                                  // or, where a multipart stream writes none, one for its items,
                                  // each a part: whether it is a file, and its default content
                                  // type; singular like Item, because it governs every item alike.
                                  // Positional per-item encoding (3.2 prefixEncoding) has no form
                                  // here and stays in Unmodeled — see docs/ir/unmodeled.md
    Encoding  map[PropID]PartEncoding // multipart/form: per-property (part) wire config, keyed by the
                                  // part property's PropID — a position inside the body model, so
                                  // `pass` resolves a key against that model rather than a
                                  // registry; of a part several declarations name, the one the
                                  // model's flattening takes (docs/ir/models.md). A body that is a
                                  // union, as one a composition is distributed across is, has its
                                  // variants' parts, through the unions nested in them, a part keyed
                                  // under each variant declaring it, each entry as that variant
                                  // declares the part; and a body that is a model narrowing a
                                  // union, or a variant that is one, each shape's parts
                                  // (docs/ir/models.md)
    File      *FileInfo           // body is a file upload/download (TypeSpec file bodies, binary
                                  // payloads): how it travels, while Type stays what the source
                                  // declares of it, so nothing that declaration says is lost
    Examples  []Example
    Docs      Docs                // this media type's own documentation, as against the body's:
                                  // an OpenAPI 3.2 Media Type Object's description, and a summary
                                  // or description written beside a $ref to one, which documents
                                  // that use of it. Omitted when empty, as Payload.Docs is
    Unmodeled  Unmodeled
}

type PartEncoding struct {
    ContentTypes []string          // media type(s) of this part: as the source declares them, or,
                                   // where it declares none, the default its format gives the value
                                   // the encoding applies to, each item of a repeated part. Empty
                                   // where the format gives none. Not read where Style is set,
                                   // whose arrangement replaces it, and given no default there
    Headers      []Header          // per-part headers
    Multi        bool              // part repeats, or may: an array member, or a union with a
                                   // list among its variants → repeated parts
    Filename     bool              // part carries a filename (file part); a part of a url-encoded
                                   // body, which names no file, never does
    Inferred     Inferred          // "" = the source declares the part a file; else the heuristics
                                   // that read Filename in (OpenAPI's binary media types), so a
                                   // consumer taking only what was declared can set it aside. Set
                                   // only where Filename is (ir/part-encoding-inferred-not-filename)
    Style        string            // set wherever the source arranges the part's value as a query
                                   // parameter's is (HTTPParamBinding.Style), the style it names or
                                   // the one its format defaults to, so ContentTypes is not read;
                                   // "" = the value travels as ContentTypes says
    Explode      *bool             // as declared beside Style; nil reads as Style's default
    AllowReserved bool             // the part's value is written as HTTPParamBinding.AllowReserved
                                   // writes a parameter's: reserved characters and percent-encoded
                                   // triples pass unescaped. Set only beside Style
}
// TypeSpec tuple-form multipart lowers to a synthesized model whose properties are the parts.

type FileInfo struct {
    IsText   bool                 // textual vs binary contents
    Contents *TypeRef             // declared contents scalar chain (string/bytes extensions); nil = bytes.
                                  // A compiler's lowered schema stays on Content.Type: it need not
                                  // be such a chain, as a type array lowers to a union
    ContentTypes []string         // declared *allowed* content-type set (TypeSpec File<"image/png" |
                                  // "image/jpeg">); runtime Content-Type comes from the file value
    ContentTypeDefault string     // default when the file value carries none
    FilenameLocation string       // "content-disposition" (default) | "path" | "header"
    FilenameWireName string       // wire name when FilenameLocation is path/header
    Inferred Inferred             // "" = the source declares the body a file; else the heuristics
                                  // that read it as one, as an OpenAPI body whose schema says no
                                  // more than "a string" is read on a binary media type. Type then
                                  // stays what the schema says, so a consumer setting the guess
                                  // aside reads the body as declared
}

type ContentStream struct {
    Inferred Inferred             // "" = the source declares the content a stream; else the
                                  // heuristics that read it as one, as OpenAPI's streaming media
                                  // types are
}
```

## A content that streams

A body is one document or a sequence of frames, and which is a fact about one content: an OpenAPI
response can offer `application/json` beside `text/event-stream`, one value and a stream of them,
the request choosing by its `Accept` or by an input
([streaming](./interaction-patterns.md#streaming)). `Content.Stream` is set on every content that
streams, and on no other, wherever the content sits: a request, a response, an error case, the
`Default` or a message. A content declaring an `Item` streams, so it sets `Stream` too
(`ir/content-item-without-stream`), and a consumer asks `Stream` alone whether a content streams,
and `Item` what each of its items is where the source says. An OpenAPI document below 3.2 states a
stream only by naming a media type, so the compiler's streaming policy reads one in, and
`Stream.Inferred` names the heuristic, as `FileInfo.Inferred` does for a file
([provenance](./provenance.md)); a consumer taking only what was declared sets such a content's
stream aside. `Operation.Streaming` and its `StreamDetail`s summarize the contents of a request and
of the success responses that stream, so such a content has its side's `StreamDetail`
(`ir/content-stream-without-detail`). The converse is not held: a side may stream with no content at
all, as an RPC whose input folds into its parameters does, and an error's or the `Default`'s content
is summarized by no `StreamDetail`, so whether it is read frame by frame is the plan's
([streams](../emitters/plan/streams.md)).

Four other homes were weighed:

- **`Item` with `TypeRef.Inferred`.** Setting `Item` on every content the policy reads as a stream
  would change what a declared `Item` means. A plan decodes each item into `Item` and reads an SSE
  item as the event itself ([streams](../emitters/plan/streams.md)), while a schema of the whole
  body only says what its frames hold, and may say no more than that the body is text: a
  bare-string stream would gain an `Item` of `string` and decode each line as a JSON string. `TypeRef.Inferred`
  speaks only of null, so it could not mark the difference either.
- **A list on `StreamDetail` naming the contents that stream.** A media type does not name a
  content once an operation has several responses, and neither an error nor the `Default` has a
  `StreamDetail` to hold one.
- **No field, the consumer reading the media type.** A consumer re-runs a heuristic the compiler
  already ran, under a table of its own, and the two disagree wherever the tables do. With the
  compiler told that `application/x-acme` streams, a response offering it beside `application/json`
  has a `StreamDetail` and no content the plan's table names, so the plan elects among all its
  contents and streams the JSON one. And a media type is read only beside a `StreamDetail`, so a
  `Default` offering `text/event-stream` alone, which no `StreamDetail` summarizes, would reach the
  plan as one document.
- **A framing on the content.** How a stream divides into frames is the binding's
  ([streaming](./interaction-patterns.md#streaming)): over HTTP the content's `MediaType` states it,
  and another binding frames its own messages. `Stream` says only that the content streams.
