// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package cache keeps a run's entries between processes: Dir is a directory of
// them, each named by a Key, no larger in all than MaxBytes. BuildID names the
// running build, which a key must cover. What an entry holds, and whether it
// still answers a run, are the engine's to say (engine.Cache).
package cache
