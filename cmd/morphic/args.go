package main

import (
	"flag"
	"strings"
)

// flagTerminator is the end-of-flags marker every POSIX utility accepts: the
// arguments after it are operands however they are spelled, which is the only
// way to name a file that begins with "-".
const flagTerminator = "--"

// boolFlag is the flag package's own test for a flag set by its own presence
// rather than by the argument after it. The interface is unexported there, so
// it is restated rather than reached for; every flag registered by BoolVar
// satisfies it.
type boolFlag interface{ IsBoolFlag() bool }

// cutTerminator returns args with a leading flagTerminator removed, and reports
// whether one was there. It is the whole of terminator handling for an argument
// list that defines no flags: there is nothing to stop parsing, so the marker's
// only job is to say that what follows is not a flag.
func cutTerminator(args []string) ([]string, bool) {
	if len(args) > 0 && args[0] == flagTerminator {
		return args[1:], true
	}
	return args, false
}

// splitAtTerminator splits args at the first flagTerminator standing as an
// argument of its own, returning what precedes it and the operands that follow.
// A "--" some flag asked for is that flag's value and not a marker, so the scan
// steps over it — which is the whole reason it needs fs rather than a plain
// search for the token.
func splitAtTerminator(fs *flag.FlagSet, args []string) (before, operands []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == flagTerminator {
			return args[:i], args[i+1:]
		}
		if takesNextValue(fs, args[i]) {
			i++
		}
	}
	return args, nil
}

// takesNextValue reports whether arg is a flag fs defines that reads its value
// from the following argument: spelled without an inline "=value" and not
// boolean. An argument fs does not define is left alone, since Parse will
// reject it and the split cannot change that.
func takesNextValue(fs *flag.FlagSet, arg string) bool {
	name, ok := flagName(arg)
	if !ok {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	b, isBool := f.Value.(boolFlag)
	return !isBool || !b.IsBoolFlag()
}

// flagName returns the flag name arg spells, and whether arg is a flag whose
// value would come from the next argument at all. It mirrors the flag package's
// own syntax: one or two leading dashes, a name that starts with neither "-"
// nor "=", and no inline "=value".
func flagName(arg string) (string, bool) {
	if len(arg) < 2 || arg[0] != '-' {
		return "", false
	}
	name := strings.TrimPrefix(arg[1:], "-")
	if name == "" || name[0] == '-' || name[0] == '=' {
		return "", false
	}
	if strings.Contains(name, "=") {
		return "", false
	}
	return name, true
}
