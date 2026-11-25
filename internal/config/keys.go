package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnknownKeyError is the one shape of every unknown-key error, from config blocks to workflow steps.
func UnknownKeyError(line int, key, path string, known []string) error {
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
		return fmt.Errorf("%s, did you mean %q?", msg, best)
	}
	return fmt.Errorf("%s; expected one of %s", msg, strings.Join(known, ", "))
}

// pathDecoder is a custom decoder that checks its own keys and reports them under the given path.
type pathDecoder interface {
	decode(node *yaml.Node, path string) error
}

// checkKeys walks the YAML tree against the config types; KnownFields(true) alone
// reports neither the key path nor the likely fix. open guards against alias cycles.
func checkKeys(node *yaml.Node, t reflect.Type, path string, open map[*yaml.Node]bool) error {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	node = resolve(node)
	if open[node] || node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	open[node] = true
	defer delete(open, node)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if d, ok := reflect.New(t).Interface().(pathDecoder); ok {
		return d.decode(node, path)
	}
	if u, ok := reflect.New(t).Interface().(yaml.Unmarshaler); ok {
		if err := u.UnmarshalYAML(node); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
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
		for _, p := range mappingPairs(node) {
			keyPath := joinKey(path, p.name)
			fieldType, ok := fields[p.name]
			if !ok {
				if ineffectiveKeys[keyPath] {
					return fmt.Errorf("line %d: %s was removed: it never had an effect; delete it", p.line, keyPath)
				}
				return UnknownKeyError(p.line, p.name, path, slices.Collect(maps.Keys(fields)))
			}
			if err := checkKeys(p.value, fieldType, keyPath, open); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, p := range mappingPairs(node) {
			if err := checkKeys(p.value, t.Elem(), joinKey(path, p.name), open); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return nil // the typed decode reports the type mismatch
		}
		for i, item := range node.Content {
			if err := checkKeys(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i), open); err != nil {
				return err
			}
		}
	}
	return nil
}

type keyPair struct {
	name  string
	line  int
	value *yaml.Node
}

func resolve(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}
	return node
}

// mappingPairs lists a mapping's entries the way yaml.v3 decodes them: aliases followed and
// merge keys expanded, explicit keys winning over merged ones and earlier merge sources over later.
// A key keeps the line where it is written, inside the anchored mapping for merged keys.
func mappingPairs(node *yaml.Node) []keyPair {
	return collectPairs(resolve(node), map[*yaml.Node]bool{})
}

func collectPairs(node *yaml.Node, open map[*yaml.Node]bool) []keyPair {
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
				merged = append(merged, collectPairs(resolve(source), open)...)
			}
			continue
		}
		pairs = append(pairs, keyPair{name: resolve(key).Value, line: key.Line, value: value})
	}
	for _, m := range merged {
		if !slices.ContainsFunc(pairs, func(p keyPair) bool { return p.name == m.name }) {
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
