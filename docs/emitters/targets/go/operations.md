# Operations in Go

## The request type

Go's request type ([request shaping](../../refine/requests.md)) is a `<Operation>Params` struct. An
input no style arranges ([values as text](../../refine/text.md#a-value-no-style-arranges)) is a
`string` field, or a `[][2]string` of names and values where its position carries pairs, a pair
whose name the input does not own being the call's `error` return, naming the input, nothing sent. A
form or multipart body gives the struct a field for each of its members in place of one holding the
body, a caller writing `UploadParams{File: f, Title: &t}` ([the
body](../../refine/requests.md#the-body)).

## Pagination

The IR, the plan and what every target generates are in
[pagination](../../operations/pagination.md).

- **refine.** `BuildRequestShaping` builds the request from the query bindings. By the default
  parameter shape ([request shaping](../../refine/requests.md)), marked `Inferred`, `limit` and
  `cursor` are fields of `ListUsersParams`.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) iter.Seq2[User, error]` is a range-over-func
  auto-pager: it yields one non-nil error when a fetch fails, and fetches no further page once the
  caller's loop breaks.
- **emit.** Printer emits `ListUsers` and `ListUsersParams`; the pager needs no named iterator type.
  The pager *runtime* is `pagination.go.tmpl`.

## Streaming

The IR, the plan and what every target generates are in [streaming](../../operations/streaming.md).

`TailMetrics` has two methods: `func (c *Client) TailMetrics(ctx) (*MetricsPage, error)` and `func
(c *Client) TailMetricsStream(ctx) (*runtime.Stream[Metrics, TailMetricsStreamHeaders], error)` — no
request type, since the operation has no input ([request shaping](../../refine/requests.md)).
`Stream[T, H]` is the runtime's: `Recv() (T, error)` returns each element and then `io.EOF`, `Close`
releases the response, which a caller that stops early must do, and `Header() (H, error)` returns
the response's declared headers
([streaming](../../operations/streaming.md#what-a-target-generates)), `H` a struct named for the
method and declared even with no field. A server stream's method returns once the response's head
has arrived, so `Header` returns at once; on a stream the client writes it waits for the head, and
fails as `Recv` would where the response is a failure. Measured with Go 1.27.3, a caller in another
module, through the method of a group's unit, read both headers of a server holding its first frame
back until they were read. A `Recv` that gets no byte for `Timeout.Idle` fails with a timeout
([runtime policy](../../contract/runtime-policy.md#what-a-layer-writes)): `net/http` gives a
response's body no read deadline, so the stream and a `RawBody` read through a wrapper that cancels
the request when one read waits past the bound, and a stream the caller writes times each write the
same way. Measured with Go 1.27.3 and a bound of 300ms, a reader of a stream held open after one
frame returned at 300ms, and without the wrapper only when the server closed the stream; one whose
server wrote a comment line every 100ms read on; and a `Send` to a server that had stopped reading
returned at 300ms, and without the bound had not after 1.5s. What `T` is follows from `Recv`:

- an `Element` is what its type lowers to. A union's is its struct
  ([`Payment`](unions.md#payment-in-go)), and `Recv` returns `io.EOF` after an element holding a
  variant whose `Variant.Event.Terminal` is true, so an event union needs nothing of its own beyond
  that.
- no `Element` is `Stream[string, H]`, each frame's text, unread.
- a `bytes` side has no frames and no `Stream`: `Recv` lowers as a file does, to `*RawBody` ([return
  shapes](returns.md)), and `Send` to an `io.Reader` field of the request type.

A client stream gives `Send(T)` and a `CloseAndRecv` that returns what `Return` reads. T-24 holds
this section.

Printer emits the methods and the element's declarations. The stream *runtime* is `stream.go.tmpl`,
and one template per framing the document uses.

## Auth

Go's per-scheme option types ([auth](../../operations/auth.md)) are `WithAPIKey(...)` and
`WithOAuth2(...)`.

## Long-running operations

The IR, the plan and what every target generates are in [long-running
operations](../../operations/long-running.md).

`LowerLRO` emits a poller: `func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller,
error)`, where `ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
`ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
`ResultPath`. The poll loop is `lro.go.tmpl`, paced and bounded by `Policy.Polling` ([runtime
policy](../../contract/runtime-policy.md#what-a-layer-writes)): `PollUntilDone` returns an error
naming `MaxPolls` after that many polls, and the poller polls on if called again. Printer emits
`StartExport`, `StartExportParams`, and `ExportPoller` + `Poll()/PollUntilDone()`.

## Stability

Each method an `alpha` or `beta` operation lowers to, its `WithResponse` method among them, gives
its tier a paragraph of its doc comment after the summary
([stability](../../operations/stability.md)), such as `Beta: breaking changes are possible but not
expected.`, so the comment still opens with the sentence summarizing the method, as a Go doc comment
does, and `go doc` and an editor show the tier at the call. Go has no annotation a caller must
acknowledge, and a build tag compiling an `alpha` method only under `-tags alpha` would split the
package's API by tag, every test then building twice, so Go gates nothing.

## Operations the API sends

A service's receiver ([operations the API sends](../../operations/inbound.md)) is `Receiver`, built
by `NewReceiver(cfg ReceiverConfig)`. `ReceiverConfig` is a struct holding a field for each scheme
its operations name that the receiver can compare, named for the scheme, its zero value giving none;
`Verify func(req *http.Request, body []byte, check runtime.InboundCheck) error` for the caller's
check; and `Unauthenticated bool`, stating that an operation declaring no security admits any
sender. The check reads the method, the target, the headers and `req.TLS.PeerCertificates` from
`req`, the bytes from `body`, `req.Body` having been read, and from `check` what it stands in for:
`Operation`, the parse method as the SDK names it, as an event names a call's ([what an event
holds](../../contract/runtime-policy.md#what-an-event-holds)), `Alternative`, the alternative's
place in the operation's `Auth`, from 0, `Scheme`, the name the document gives the scheme, empty for
an alternative naming none, and `Scopes`, the scopes that use requires of it, a slice of the check's
own. The fields are a scope of the struct's own, where a scheme's field meets only `Verify` and
`Unauthenticated` and yields to them, so the unit gains `Receiver`, `NewReceiver` and
`ReceiverConfig` whatever the schemes are called. Options named as the client's are, `WithVerify`
and one for each scheme, were rejected: the receiver sits in the client's unit, which already
declares the client's option for a scheme of that name (`WithAPIKey redeclared in this block`), and
a prefix only moves where they meet, a client's scheme `receiver key` and the receiver's `key` both
rendering `WithReceiverKey`.

A parse method takes the `*http.Request` every Go HTTP server hands a handler, `func (r *Receiver)
ParseZoneCreated(req *http.Request) (*webhooks.ZoneCreatedRequest, error)`, reading its body no
further than a byte past the bound, and a refused check is an error `errors.Is` matches to the
runtime's `ErrUnverified`. A response type writes itself with `func (resp *ZoneCreated200Response)
WriteTo(w http.ResponseWriter) error`. With Go 1.27.3, a handler built from them across a root and a
group package answered a valid request 200, a wrong key and a wrong HMAC signature 401, and a valid
body past the bound 400, which with the bound removed it answered 200. Under `httptest` over TLS,
for an operation stating `[{http_basic}, {mutual_tls}]`, a receiver given the basic credentials and
a check verifying the client certificate answered correct credentials with no certificate 200, the
check not called, wrong ones with a certificate 200, the check told `mutual_tls`, and wrong ones
with none 401; a check for an operation stating no security was told no scheme and verified an HMAC
over the body; and a receiver given no key for an `apiKey`, and one given no setting for an
operation declaring no security, refused a request, both of which answered 200 when the receiver
compared the absent key or checked nothing. For an operation stating `[{oauth2: [read], api_key},
{oauth2: [admin]}]`, a check comparing the token's scopes with `Scopes` passed a `read` token with
the key, told `oauth2` once, at 0 with `read`, and without the key refused it and passed an `admin`
token, told that and then `oauth2` at 1 with `admin`; asked once for the scheme, it passed the
`read` token without the key. The same package declared the client's `WithAPIKey` and, for a scheme
named `verify`, its `WithVerify`. T-71 holds this section.

## The client

The client's settings ([the client](../../operations/client.md)) are functional options —
`NewClient(WithRegion(RegionEU))`, `WithServer(…)`, `WithBaseURL(u)`, and `WithHook(h)` for the hook
its events go to ([what an event holds](../../contract/runtime-policy.md#what-an-event-holds)). A
[client setting](../../operations/client.md#client-settings) is one more, `WithAccountID("…")`, and
the request type's field for it a pointer, `AccountID *string`, nil taking the client's.
`WithTransport(t http.RoundTripper)` replaces what sends a request, `net/http`'s by default, so a
wrapper is a `RoundTripper` over the one it replaces, and a scheme's option that takes the caller's
code takes an `Authenticator`, `interface{ Authenticate(req *http.Request) error }`
([auth](../../operations/auth.md#where-a-credential-can-be-read)). The policy's members a caller
sets ([runtime policy](../../contract/runtime-policy.md#what-a-layer-writes)) are options as well,
`WithMaxAttempts`, `WithBackoff`, `WithThrottledBackoff`, `WithMaxRedirects`, `WithRequestTimeout`,
`WithIdleTimeout`, `WithMaxBody`, `WithAutoPageDelay`, `WithMaxPages`, `WithPollBackoff` and
`WithMaxPolls`, a duration a `time.Duration` and a backoff the runtime's `Backoff{Initial, Max
time.Duration; Multiplier, Jitter int}`, its ratios in thousandths, a `Multiplier` of 2000 doubling
each wait. `c.WithOptions(opts ...Option) (*Client, error)` returns a client holding `c`'s settings
with those replaced, sending through `c`'s transport and counting its calls in `c`'s `Event.Call`,
as each sub-client does, for one call or several; its name is reserved among the client's members
([naming](naming.md#reserved-names)).
