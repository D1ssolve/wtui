package config

import (
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"reflect"
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
		return &keyError{line, column, fmt.Sprintf("%s, did you mean %q?", msg, best)}
	}
	return &keyError{line, column, fmt.Sprintf("%s; expected one of %s", msg, strings.Join(known, ", "))}
}

func unsupportedKey(key *yaml.Node, path string) *keyError {
	out, _ := yaml.Marshal(resolve(key))
	msg := "unsupported key " + strings.Join(strings.Fields(string(out)), " ")
	if path != "" {
		msg += " in " + path
	}
	return &keyError{key.Line, key.Column, fmt.Sprintf("line %d: %s; keys must be plain strings", key.Line, msg)}
}

// keyName is the key as yaml.v3 decodes it into a string: aliases followed, !!binary
// decoded, an untagged number, bool or timestamp named by its source text (so a service
// or state may be called 123). ok is false for null keys, which yaml.v3 silently drops
// from maps, for explicit non-string tags and for sequence or mapping keys.
func keyName(key *yaml.Node) (name string, ok bool) {
	key = resolve(key)
	if key.Kind != yaml.ScalarNode {
		return "", false
	}
	switch tag := key.ShortTag(); {
	case tag == "!!str":
		return key.Value, true
	case tag == "!!binary":
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(key.Value), ""))
		return string(data), err == nil
	case key.Style&yaml.TaggedStyle == 0 && tag != "!!null":
		return key.Value, true
	}
	return "", false
}

// pathDecoder is a custom decoder that checks its own keys and reports them under the given path.
type pathDecoder interface {
	decode(node *yaml.Node, path string) error
}

// keyWalk checks the YAML tree against the config types; KnownFields(true) alone reports
// neither the key path nor the likely fix. Removed-key hints and other key problems are
// collected document-wide so that the earliest hint always wins over any other problem.
type keyWalk struct {
	open   map[*yaml.Node]bool // guards against alias cycles
	hints  []*keyError
	issues []*keyError
}

func (w *keyWalk) err() error {
	for _, found := range [][]*keyError{w.hints, w.issues} {
		if len(found) > 0 {
			return slices.MinFunc(found, func(a, b *keyError) int {
				return cmp.Or(cmp.Compare(a.line, b.line), cmp.Compare(a.column, b.column))
			})
		}
	}
	return nil
}

func (w *keyWalk) issue(err error, node *yaml.Node) {
	at := &keyError{line: node.Line, column: node.Column}
	if ke := (*keyError)(nil); errors.As(err, &ke) {
		at = ke
	}
	w.issues = append(w.issues, &keyError{at.line, at.column, err.Error()})
}

// structural is true while path consists of struct fields only: removed-key hints match
// there and nowhere else, so a key literally named "prune.fetch" never matches one.
func (w *keyWalk) check(node *yaml.Node, t reflect.Type, path string, structural bool) {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	node = resolve(node)
	if w.open[node] || node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return
	}
	w.open[node] = true
	defer delete(w.open, node)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if d, ok := reflect.New(t).Interface().(pathDecoder); ok {
		if err := d.decode(node, path); err != nil {
			w.issue(err, node)
		}
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
			if name != "" && name != "-" {
				fields[name] = t.Field(i).Type
			}
		}
		for _, p := range mappingPairs(node, path) {
			if p.err != nil {
				w.issues = append(w.issues, p.err)
				continue
			}
			if path == "" && strings.HasPrefix(p.name, "x-") {
				continue // top-level x-* keys are opaque anchor holders (the Compose convention)
			}
			keyPath := joinKey(path, p.name)
			if structural && !strings.Contains(p.name, ".") {
				if hint := removedKeyHint(keyPath); hint != "" {
					w.hints = append(w.hints, &keyError{p.line, p.column, fmt.Sprintf("line %d: %s", p.line, hint)})
					continue
				}
			}
			fieldType, ok := fields[p.name]
			if !ok {
				w.issues = append(w.issues, unknownKey(p.line, p.column, p.name, path, slices.Collect(maps.Keys(fields))))
				continue
			}
			w.check(p.value, fieldType, keyPath, structural)
		}
	case reflect.Map:
		for _, p := range mappingPairs(node, path) {
			if p.err != nil {
				w.issues = append(w.issues, p.err)
				continue
			}
			w.check(p.value, t.Elem(), joinKey(path, p.name), false)
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return // the typed decode reports the type mismatch
		}
		for i, item := range node.Content {
			w.check(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i), false)
		}
	}
}

// maxExpandedNodes bounds the work of walking a document whose aliases repeat content;
// real configs have a few hundred nodes.
const maxExpandedNodes = 100_000

// expandedSize counts the nodes a full alias expansion visits, capped above maxExpandedNodes.
func expandedSize(node *yaml.Node, memo map[*yaml.Node]int) int {
	if size, ok := memo[node]; ok {
		return size
	}
	memo[node] = 0 // a cycle adds nothing; the walk's open set stops it
	size := 1
	children := node.Content
	if node.Kind == yaml.AliasNode {
		children = []*yaml.Node{node.Alias}
	}
	for _, child := range children {
		size = min(size+expandedSize(child, memo), maxExpandedNodes+1)
	}
	memo[node] = size
	return size
}

type keyPair struct {
	name         string
	line, column int
	value        *yaml.Node
	err          *keyError // set when the key does not decode to a string
}

func resolve(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}
	return node
}

// mappingPairs lists a mapping's entries the way yaml.v3 decodes them: keys by decoded value,
// aliases followed and merge keys expanded, explicit keys winning over merged ones and earlier
// merge sources over later. A key keeps the position where it is written, inside the anchored
// mapping for merged keys. path names the mapping in unsupported-key errors.
func mappingPairs(node *yaml.Node, path string) []keyPair {
	return collectPairs(resolve(node), path, map[*yaml.Node]bool{})
}

func collectPairs(node *yaml.Node, path string, open map[*yaml.Node]bool) []keyPair {
	if node.Kind != yaml.MappingNode || open[node] {
		return nil
	}
	open[node] = true
	defer delete(open, node)
	var pairs, merged []keyPair
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.Tag == "!!merge" {
			sources := []*yaml.Node{resolve(value)}
			if sources[0].Kind == yaml.SequenceNode {
				sources = sources[0].Content
			}
			for _, source := range sources {
				merged = append(merged, collectPairs(resolve(source), path, open)...)
			}
			continue
		}
		pair := keyPair{line: key.Line, column: key.Column, value: value}
		if name, ok := keyName(key); ok {
			pair.name = name
		} else {
			pair.err = unsupportedKey(key, path)
		}
		pairs = append(pairs, pair)
	}
	seen := map[string]bool{}
	for _, p := range pairs {
		seen[p.name] = p.err == nil
	}
	for _, m := range merged {
		if m.err != nil || !seen[m.name] {
			seen[m.name] = m.err == nil
			pairs = append(pairs, m)
		}
	}
	return pairs
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
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
