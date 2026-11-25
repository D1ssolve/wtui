package config

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// keyError is a key problem with the position Load uses to report the earliest one.
type keyError struct {
	line, column int
	msg          string
}

func (e *keyError) Error() string { return e.msg }

// UnknownKeyError is the one shape of every unknown-key error, from config blocks to workflow steps.
func UnknownKeyError(line int, key, path string, known []string) error {
	return unknownKey(line, 0, key, path, known)
}

func unknownKey(line, column int, key, path string, known []string) *keyError {
	msg := fmt.Sprintf("unknown key %q", key)
	if path != "" {
		msg += " in " + path
	}
	if line > 0 {
		msg = fmt.Sprintf("line %d: %s", line, msg)
	}
	known = slices.Sorted(slices.Values(known))
	best, bestDistance := "", 3 // suggest only keys within edit distance 2
	for _, name := range known {
		if d := editDistance(key, name); d < bestDistance {
			best, bestDistance = name, d
		}
	}
	if best != "" {
		msg += fmt.Sprintf(", did you mean %q?", best)
	} else if len(known) > 0 {
		msg += "; expected one of " + strings.Join(known, ", ")
	}
	return &keyError{line, column, msg}
}

func unsupportedKey(key *yaml.Node, path string) *keyError {
	out, _ := yaml.Marshal(resolve(key))
	msg := "unsupported key " + strings.Join(strings.Fields(string(out)), " ")
	if path != "" {
		msg += " in " + path
	}
	return &keyError{key.Line, key.Column, fmt.Sprintf("line %d: %s; keys must be plain strings", key.Line, msg)}
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur := make([]int, len(rb)+1)
		cur[0] = i + 1
		for j := range rb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
