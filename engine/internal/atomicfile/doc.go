// Package atomicfile replaces a file's content atomically: the new content is
// written to a temporary file beside the destination, flushed, and renamed over
// it, so a reader sees the whole previous content or the whole new one, and a
// failure, or a cancellation before the rename, leaves the previous content in
// place.
//
// github.com/google/renameio/v2 creates, syncs and renames the file. This
// package fixes where the temporary file goes and the mode it holds, which
// renameio leaves to options, and names the destination in every error. The
// library builds for no Windows target, so neither does this package.
package atomicfile
