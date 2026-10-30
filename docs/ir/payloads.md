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
    Item      *TypeRef            // element shape of a sequential stream declared per media type
                                  // (OpenAPI 3.2 itemSchema for SSE/JSONL/json-seq); nil = not sequential
    ItemEncoding *PartEncoding    // wire encoding of a sequential stream's item (3.2 itemEncoding);
                                  // singular like Item, because it governs every item alike.
                                  // Positional per-item encoding (3.2 prefixEncoding) has no form
                                  // here and stays in Unmodeled — see docs/ir/unmodeled.md
    Encoding  map[PropID]PartEncoding // multipart/form: per-property (part) wire config, keyed by the
                                  // part property's PropID — a position inside the body model, so
                                  // `pass` resolves a key against that model rather than a
                                  // registry; of a part several declarations name, the one the
                                  // model's flattening takes (docs/ir/models.md)
    File      *FileInfo           // body is a file upload/download (TypeSpec file bodies, binary payloads)
    Examples  []Example
    Unmodeled  Unmodeled
}

type PartEncoding struct {
    ContentTypes []string          // media type(s) of this part
    Headers      []Header          // per-part headers
    Multi        bool              // part repeats (array member → repeated parts)
    Filename     bool              // part carries a filename (file part)
    Style        string            // form-style serialization for non-file parts
    Explode      *bool
    AllowReserved bool             // reserved characters pass unescaped in the part's value, as
                                   // HTTPParamBinding.AllowReserved does in a parameter's
}
// TypeSpec tuple-form multipart lowers to a synthesized model whose properties are the parts.

type FileInfo struct {
    IsText   bool                 // textual vs binary contents
    Contents *TypeRef             // declared contents scalar chain (string/bytes extensions); nil = bytes
    ContentTypes []string         // declared *allowed* content-type set (TypeSpec File<"image/png" |
                                  // "image/jpeg">); runtime Content-Type comes from the file value
    ContentTypeDefault string     // default when the file value carries none
    FilenameLocation string       // "content-disposition" (default) | "path" | "header"
    FilenameWireName string       // wire name when FilenameLocation is path/header
}
```
