// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Command morphic-filetimes dates every tracked file in a clean worktree from
// its content. Go's test cache keys a result on the size, mode and time of each
// file a test opens, and a checkout stamps each with the moment it was made, so
// two checkouts of one commit share no result.
//
// A file gets a time derived from its blob hash, and a directory holding one a
// fixed time. It refuses a dirty worktree and an entry it cannot date, and then
// changes nothing.
//
// It dates the top level's .git entry last, since Go hashes every entry of a
// directory a test lists, the root among them, and any later git status, such
// as go build's VCS stamping, moves it again. A symlinked .git gets its target
// dated, not the link, which is out of scope.
//
// Exit code is 0 when done, 1 when refused or failed, and 2 on a usage error.
package main
