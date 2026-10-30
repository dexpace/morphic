# The client and its base URL

IR: `Document.Servers = {"server/openapi/servers/0":
{URLTemplate:"https://{region}.api.example.com/v1", Variables:[{Name:region, Default:"us",
Enum:[us, eu]}]}, …}`, a service whose `Servers` is `["server/openapi/servers/0"]`, and an
operation whose `Servers` names a server of its own.

- **Which servers.** A service's servers are those its `Servers` names, in order; one naming none
  has no declared server, and its client takes a base URL from its caller (ir-design §10). The
  client's default is the first of them a client can reach (below). The IR states both, so the plan
  copies neither (§3.6).
- **refine.** The client takes three settings beside its runtime policy: which declared server to
  use, the default unless the caller names another; a value for each of that server's variables, its
  `Default` unless the caller gives one, and one of its `Enum` when it has one, each value of which
  is a constant the caller can name; and a base URL, which replaces the server's URL entirely, for a
  proxy, a test server or a deployment the document does not list
  ([Go](../targets/go/operations.md#the-client), [Kotlin](../targets/kotlin/operations.md#the-client)). A value outside a variable's `Enum` is refused when the client is built,
  never sent. An empty `Default` substitutes nothing, the one reading OpenAPI admits, since it
  requires a default; telling an empty default from none, which AsyncAPI allows, is #883's.
- **A server a client cannot reach.** A server whose URL, its variables at their defaults, has no
  scheme and host — the `/` OpenAPI supplies for a document that declares no server, which records
  no pointer (ir-design §10), among them — cannot be the default: the document's location, which
  OpenAPI resolves a relative URL against, is not something an SDK knows. The default is the first
  server that has both, so a relative server listed first passes the default to the next. A client
  refuses to be built while the server it would use has neither and no base URL replaces it, as when
  a caller names a relative server, and refine reports a client none of whose servers has both once,
  as an info `emitters/<target>/no-default-server`.
- **An operation's own servers.** An operation whose `Servers` names any is sent to those in place
  of its service's, by the same rules: to the first of them a client can reach, each variable at its
  `Default`. The caller's choice of server and of variable values is a choice among the service's
  servers, so it does not reach them; a base URL the caller gives replaces every server's URL, an
  operation's own included, since a proxy or a test server stands in for the whole API. An operation
  none of whose servers a client can reach refuses to be called unless a base URL replaces them, and
  refine reports it once as an info `emitters/<target>/no-default-server`, naming the operation.
- **Auth.** An HTTP target authenticates as §8.6 says, from the operation and the service.
  `Server.Auth` is the placement the messaging formats use, where a broker connection authenticates
  per server, and a messaging target reads it.
- **emit.** The URL template is expanded at request time from the chosen values; the template and
  each variable's constants are typed AST, the expansion a runtime helper.
