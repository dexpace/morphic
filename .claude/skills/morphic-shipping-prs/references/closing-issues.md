# Closing issues

Which issues a pull request closes, and how to show each is finished before a report says it is.

## Close only the issues the PR finishes, one keyword each
`closing-keywords`

- **Rule.** On merge, GitHub closes each issue that `close`, `fix` or `resolve`, in any tense and
  case, colon or not, precedes as `#N` or `<owner>/<repo>#N` anywhere in the body, and a
  keyword reaches only the reference after it. Write one `Closes #N` line per finished issue,
  narrate other work with other verbs ("landed"), and give a stack landing through one PR every
  child's `Closes #N`: a keyword links only in a PR based on `main`. A sidebar link closes one too
  (`references/closing-issues.md#closing-sidebar-link`).
- **Why.** "closed #907", written about another PR, made #948 claim #907; #1016 wrote a line per
  issue, since a list after one keyword closes its first alone; #464 left its stack's issues open.
- **Probe.** Read each (`references/closing-issues.md#closing-issue-read-whole`), and compare the
  numbers by `references/closing-issues.md#closing-sidebar-link`.

## Unlink from the sidebar an issue the PR does not finish
`closing-sidebar-link`

- **Rule.** An issue linked in the PR's Development sidebar closes on merge with no keyword in the
  body. A partly addressed issue is unlinked there as well as left without a keyword.
- **Why.** #1498's body only referenced #604, whose union-branch half it left open, while a sidebar
  link would have closed #604 on its merge (#1658).
- **Probe.** Seconds after each body edit, as the field lags, compare
  `gh pr view <N> --json closingIssuesReferences` with the body's:
  `grep -noiE '\b(close[sd]?|fix(e[sd])?|resolve[sd]?):? +([[:alnum:]_.-]+/[[:alnum:]_.-]+)?#[0-9]+'`.
  A number only the first lists is a sidebar link, removed or given a `Closes` line.

## Read each issue a PR closes whole, and again before saying it closes
`closing-issue-read-whole`

- **Rule.** For each issue the PR closes, by keyword or sidebar link
  (`references/closing-issues.md#closing-keywords`), read
  `gh issue view <number or URL> --json state,stateReason,body,comments,updatedAt` when the PR opens
  and again before a report says the PR closes it. A closed issue's closing comment is a recorded
  decision, its reference leaving the body; a comment or body edit is part of the ask; each
  `## Out of scope` item or deferred alternative naming pending work goes to an open issue, stated
  where `references/commits-and-prs.md#limitation-stated-where-it-lives` places it.
- **Why.** #1210 kept `Closes #1251` after #1251 closed as not planned, and missed asks in eight of
  its issues' comments or later edits; closing #1433 would have stated an out-of-scope case nowhere
  (#1210, #1435).
- **Probe.** The report gives each closing issue's `updatedAt` as last read, none newer, then
  `references/closing-issues.md#closing-issue-met-at-its-site`.

## Hold a closing issue's title and Direction to the head
`closing-issue-met-at-its-site`

- **Rule.** Before a report says a PR closes an issue, read its title against the site it names on
  the head, where a defect it states must now be false, shown by its reproduction where it gives
  one (`references/closing-issues.md#closing-issue-reproduction-reruns`), an imperative done and a
  question answered, and match each step of its Direction to the line meeting it, or say in the
  body why none does.
- **Why.** At 78c30a744, #1522 closed #1294, #1326, #1348, #1362, #1461 and #1495 while the rule
  each named, in its title or its Direction, read as before, the fix sitting in a new rule the old
  one never cited, and dropped #1347's step running the gate steps after a failure (#1522).
- **Probe.** For each closing issue, quote the site its title names, as it reads at the head.
  Then `references/closing-issues.md#closing-issue-cited-in-the-tree`.

## Find what in the tree still describes a closing issue as open
`closing-issue-cited-in-the-tree`

- **Rule.** For each issue the PR closes, `git grep` the head for its number and for the input its
  reproduction writes, and read each hit. A sentence calling the issue open, planned or
  pending, or taking its input as an example of the old behaviour, is rewritten on the branch; a
  test or comment citing it as the reason for a fix stays.
- **Why.** At #1361's 3d7252950, after a round read each closing issue at its site, three documents
  still called #664, #668 and #987 open, one stating a rule the new field contradicted; at
  894da789c, #1647 kept two examples quoting #1309's input, which no number found (#1642).
- **Probe.** Over the closing issues' numbers,
  `git grep -nE '(#|issues/)(<N>|<M>)([^0-9]|$)' -- . ':!*.golden*' ':!docs/ir/version-history.md'`,
  then `git grep -nF '<a line of each reproduction>'`.

## Rerun a closing issue's reproduction on the base and on the head
`closing-issue-reproduction-reruns`

- **Rule.** Where an issue a PR closes reproduces its defect, by a script, an input or a command
  with its output, run it on the PR's base and on its head: the defect shows on the base and not
  on the head, or the report says why not. Reading the site its title names clears a fix
  made at one site of the mechanism while another beside it keeps the defect.
- **Why.** At #1619's 51bcb107, the review setup pinned `main=$(git rev-parse origin/main)` and
  merged `"$main"`, which read as meeting #1590's title, while its first merge still read
  `origin/$base`, `origin/main` itself for a PR on `main`, and #1590's reproduction failed that
  merge as it failed `main`'s (#1627).
- **Probe.** The report quotes each reproduction's output on the base and on the head.
