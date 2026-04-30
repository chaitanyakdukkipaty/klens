package main

import "strings"

// reorderArgs reorders CLI arguments so that flags appear before positional
// arguments, enabling kubectl-style mixed usage like "klens get pods -o json".
//
// Go's flag package stops parsing at the first non-flag argument, so without
// this helper "get pods -o json" would leave "-o json" unparsed.
//
// boolFlags lists flag names that take no value (e.g. "f", "follow").
func reorderArgs(args []string, boolFlags map[string]bool) []string {
	var flags, positionals []string
	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			i++
			continue
		}
		// Self-contained form: -flag=value or --flag=value.
		if strings.Contains(arg, "=") {
			flags = append(flags, arg)
			i++
			continue
		}
		// Strip one or two leading dashes to get the flag name.
		name := strings.TrimLeft(arg, "-")
		if boolFlags[name] {
			flags = append(flags, arg)
			i++
			continue
		}
		// Consume flag + value pair.
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, arg, args[i+1])
			i += 2
		} else {
			flags = append(flags, arg)
			i++
		}
	}
	return append(flags, positionals...)
}
