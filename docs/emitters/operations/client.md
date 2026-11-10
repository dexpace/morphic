# The client and its base URL

IR: `Document.Servers = {"server/openapi/servers/0":
{URLTemplate:"https://{region}.api.example.com/v1", Variables:[{Name:region, Default:"us",
Enum:[us, eu]}]}, …}`, a service whose `Servers` is `["server/openapi/servers/0"]`, and an
operation whose `Servers` names a server of its own.

- **Which servers.** A service's servers are those its `Servers` names, in order; one naming none
  has no declared server, and its client takes a base URL from its caller
  ([servers](../../ir/servers.md)). The client's default is the first of them a client can reach
  (below). The IR states both, so the plan copies neither ([plan or
  refine](../plan/plan-or-refine.md)).
- **refine.** The client takes these settings beside its runtime policy and its [client
  settings](#client-settings): which declared server to use, the default unless the caller names
  another; a value for each of that server's variables, its `Default` unless the caller gives one,
  and one of its `Enum` when it has one, each value of which is a constant the caller can name; and
  a base URL, which replaces the server's URL entirely, for a proxy, a test server or a deployment
  the document does not list ([Go](../targets/go/operations.md#the-client),
  [Kotlin](../targets/kotlin/operations.md#the-client)). A value outside a variable's `Enum` is
  refused when the client is built, never sent, and so is a variable with no `Default`, which
  AsyncAPI allows, that the caller gives no value. An empty `Default` substitutes nothing, as
  OpenAPI's `default: ""` does.
- **A server a client cannot reach.** A server whose URL, its variables at their defaults, has no
  scheme and host — the `/` OpenAPI supplies for a document that declares no server, which records
  no pointer ([servers](../../ir/servers.md)), among them — cannot be the default: the document's
  location, which OpenAPI resolves a relative URL against, is not something an SDK knows. Nor can a
  server with a variable that has no `Default`, whose URL has no value until a caller gives one. The
  default is the first server neither excludes, so a relative server listed first passes the default
  to the next. A client refuses to be built while the server it would use has no scheme and host and
  no base URL replaces it, as when a caller names a relative server, and refine reports a client
  none of whose servers can be the default once, as an info `emitters/<target>/no-default-server`.
- **An operation's own servers.** An operation whose `Servers` names any is sent to those in place
  of its service's, by the same rules: to the first of them a client can reach, each variable at its
  `Default`. The caller's choice of server and of variable values is a choice among the service's
  servers, so it does not reach them; a base URL the caller gives replaces every server's URL, an
  operation's own included, since a proxy or a test server stands in for the whole API. An operation
  none of whose servers a client can reach refuses to be called unless a base URL replaces them, and
  refine reports it once as an info `emitters/<target>/no-default-server`, naming the operation.
- **Auth.** An HTTP target authenticates as [auth](auth.md) says, from the operation and the
  service. `Server.Auth` is the placement the messaging formats use, where a broker connection
  authenticates per server, and a messaging target reads it.
- **emit.** The URL template is expanded at request time from the chosen values; the template and
  each variable's constants are typed AST, the expansion a runtime helper.

## Client settings

A parameter an API's calls all send alike, such as an account in the path or an API version
header, is set once on the client when its service's hint names it (`ShapeHint.ClientParams`,
[shaping hints](../contract/shaping-hints.md)). Nothing makes one a setting by inference. Whether
every operation of a scope takes a parameter changes as the API grows, so a setting the plan chose
would come and go with operations, and each change would move the parameter into or out of every
method's signature: a breaking SDK change caused by an additive spec change.

- **What a setting fills.** A `ParamKey` fills, in every operation of the service, the parameter at
  its location and wire name, a header's compared without case as HTTP compares it (RFC 9110 §5.1),
  whose declaration lowers to one wire form with the others: the same root through its scalar chain
  ([scalars](../refine/scalars.md)), and the same style and explode. Requiredness, constraints and
  docs may differ, and each operation checks its own. A declaration of another wire form stays a
  plain input, reported once by refine, naming the operation, since a shared name does not make two
  parameters one. A `ParamKey` no operation of the service declares is reported, as a hint naming
  no entity is ([shaping hints](../contract/shaping-hints.md)).
- **How a call reads it.** The client takes the setting when it is built, a call may give its own,
  and a call with neither for a required parameter is refused unsent, the error naming it. A
  parameter a setting fills is not positional but an optional field of the operation's request type
  ([request shaping](../refine/requests.md)), so a call can still name another account.
- **Candidates.** The plan reports, in one info `emitters/plan/client-param-candidates`, each
  `ParamKey` that every operation of some group with more than one operation takes in one wire form,
  with the number of operations in the document taking it so, most first, so an SDK's author can
  name one. It lists one a hint already names too, since the plan reads no hints ([plan or
  refine](../plan/plan-or-refine.md)). One info per candidate was weighed and drew 82 on the golden
  spec (spec @319dc756, morphic @b206dc99), 56 of them shared by one group alone and 29 left
  optional by some operation, such as `limit`. Ranked by use, the list leads with `account_id` in
  the path, taken by 2,660 of 3,763 operations, each a string under one of 16 sets of constraints,
  and `zone_id`, taken by 698, well ahead of the paging parameter after them, `per_page`, taken
  by 186. One shared component parameter was weighed as the test instead and finds a parameter every
  operation of a group shares in 52 of the 515 groups with more than one operation, against 465 by
  location and name: the spec declares its parameters inline.
