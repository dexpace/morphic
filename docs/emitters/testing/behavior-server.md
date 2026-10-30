# The behavior server (T-14)

T-12 checks the requests an SDK builds and T-13 the timing of its runtime, both inside the process.
Much of what an SDK does shows only against a live server: honoring `Retry-After`, an event stream
split across chunk boundaries or carrying one very large event, JSON Lines framing, each pagination
strategy, status-monitor polling, OAuth token acquisition and refresh, an error body and a success
body that do not parse, a union variant the SDK does not know, a declared response a method cannot
return, a success status the document does not list, and the four combinations of required and
nullable in a response (§4.7). Writing those tests once per target does not scale, and nothing would
notice a target that left one out.

- **One server, shared by every target.** It has one route per behavior and is written by hand,
  together with the OpenAPI document that describes it, from which each target generates the SDK
  under test. It is deliberately not the mock-server emitter's output (§10): that mock is
  wire-consistent with the SDK by construction, so it would share any misreading of the spec the
  generator makes, and a test against it could not disagree with the code it tests. Each test gets
  a session of its own, so tests run in parallel, and the server records the requests each session
  received.
- **Target-neutral definitions.** Each behavior is an Arazzo workflow: the operation, the request,
  and the success criteria, written by hand. Expectations are not derived from the spec's own
  examples, which the generator under test also reads, for the same reason the server is not
  generated. A criterion about what the client sent — a second attempt no sooner than
  `Retry-After` allowed, a token refreshed once — is checked against the session's recorded
  requests. Each target supplies the runner that turns a workflow step into a call through its
  generated SDK; exact timing stays with T-13, under a fake clock.
- **A coverage gate.** Every workflow ID is registered in one place, and every target declares,
  for each, that it runs it or skips it with a reason. The gate fails when a registered ID is
  neither, when an ID a target names is not registered, and when a skipped test passes: skipped
  tests still run, and one that would pass has outlived its reason.
