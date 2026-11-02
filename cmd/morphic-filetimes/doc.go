// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Command morphic-filetimes dates every tracked file in a clean worktree from
// its content, so checkouts of one commit share Go's test results.
//
// A file gets a time derived from its blob hash, and a directory holding one a
// fixed time. The top level's .git entry is dated last, as Go hashes every entry
// of a directory a test lists and a later git status moves it. A symlinked .git
// has its target dated, which is out of scope. A dirty worktree is refused.
//
// Exit code is 0 when done, 1 when refused or failed, and 2 on a usage error.
package main
