# Security policy

## Reporting a vulnerability

Report a vulnerability privately, through GitHub's
[private vulnerability reporting](https://github.com/dexpace/morphic/security/advisories/new): on
the repository's **Security** tab, choose **Report a vulnerability**. Do not open a public issue or
pull request about it. The report opens a draft advisory that only you and the maintainers can
read, and it is followed up with you there.

A report is quickest to act on when it gives:

- the Morphic commit you ran;
- the command or library call, with the options it set;
- the smallest spec that reproduces the problem, with every document the spec references;
- what happened, and what you expected instead.

If you are unsure whether something is a vulnerability, report it privately anyway.

## Supported versions

Morphic has no released version yet. Fixes land on `main`, so only its latest commit is supported:
check that a report reproduces there.

## Scope

Morphic treats the spec it compiles, and every document the spec references, as untrusted. It
trusts what its caller supplies: the options, a file an option names (an overlay), and the code a
[library](README.md#library) caller hands it, such as a compiler, a pass or a cache.

The [README](README.md#design-principles) states the bounds Morphic holds untrusted input to:

- Unless `allow-external-refs` is set, a `$ref` reads only regular files inside the spec's
  directory, through relative references. An absolute path, a `file:` URI, a URL and a symbolic
  link out of the directory are each refused.
- [Budgets](README.md#compiler-options) on what a document or a compile may cost turn a
  pathological document into a diagnostic instead of an exhausted host.

A vulnerability is untrusted input that gets past one of these bounds: a spec that makes Morphic
read a file or reach a URL it should have refused, or a document that crashes the process, runs
without end or exhausts its memory instead of ending in a diagnostic.

Setting `allow-external-refs` says the spec is trusted to name the files and URLs it reads, so set
it only for a spec you trust. A document fetched over the network stays untrusted: one that makes
Morphic read a local file is a vulnerability.

Two things are not vulnerabilities. What a document costs under a budget the caller raised or
turned off is the caller's choice. Wrong or lossy IR for a valid spec is a bug: open a
[public issue](https://github.com/dexpace/morphic/issues) for it.

A weakness already filed under the
[`security` label](https://github.com/dexpace/morphic/issues?q=label%3Asecurity) is known and
public, so add to that issue rather than reporting it again.
