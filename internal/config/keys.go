package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var unmarshalerType = reflect.TypeFor[yaml.Unmarshaler]()

// checkKeys walks the YAML tree against the config types; KnownFields(true) alone
// reports neither the key path nor the likely fix.
func checkKeys(node *yaml.Node, t reflect.Type, path string) error {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		// Custom decoders reject their own unknown keys; running them here adds the key path to the error.
		if err := reflect.New(t).Interface().(yaml.Unmarshaler).UnmarshalYAML(node); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	}

	switch t.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil // the typed decode reports the type mismatch
		}
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
			if name != "" && name != "-" {
				fields[name] = t.Field(i).Type
			}
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Tag == "!!merge" {
				continue // the typed decode checks merged keys
			}
			keyPath := joinKey(path, key.Value)
			fieldType, ok := fields[key.Value]
			if !ok {
				return unknownKeyError(key, keyPath, slices.Sorted(maps.Keys(fields)))
			}
			if err := checkKeys(value, fieldType, keyPath); err != nil {
				return err
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := checkKeys(node.Content[i+1], t.Elem(), joinKey(path, node.Content[i].Value)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		for i, item := range node.Content {
			if err := checkKeys(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func unknownKeyError(key *yaml.Node, path string, known []string) error {
	best, bestDistance := "", 3 // suggest only keys within edit distance 2
	for _, name := range known {
		if d := editDistance(key.Value, name); d < bestDistance {
			best, bestDistance = name, d
		}
	}
	if best == "" {
		return fmt.Errorf("line %d: unknown key %q; delete it or check the spelling", key.Line, path)
	}
	return fmt.Errorf("line %d: unknown key %q, did you mean %q?", key.Line, path, best)
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
