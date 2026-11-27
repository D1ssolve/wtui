package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
)

// applyWorkflowFile publishes only the workflow entry. A nil expected snapshot means no file.
func applyWorkflowFile(target string, expected []byte, workflow string) (backup string, err error) {
	target, err = absPath(target)
	if err != nil {
		return "", err
	}
	data, err := mergeWorkflowYAML(expected, workflow)
	if err != nil {
		return "", err
	}
	return writeConfig(target, data, configPublication{expected: &expected})
}

func mergeWorkflowYAML(expected []byte, workflow string) ([]byte, error) {
	draft, err := workflowMapping([]byte(workflow))
	if err != nil {
		return nil, fmt.Errorf("read workflow draft: %w", err)
	}
	if len(draft.Content) != 2 || draft.Content[0].Value != "workflow" || draft.Content[1].Kind != yaml.MappingNode {
		return nil, errors.New("draft must contain only a top-level workflow mapping")
	}
	if _, err := gitflow.ParseWorkflowYAML(workflow); err != nil {
		return nil, fmt.Errorf("validate workflow draft: %w", err)
	}
	old, err := workflowMapping(expected)
	if err != nil {
		return nil, fmt.Errorf("read existing config: %w", err)
	}
	if old.Style&yaml.FlowStyle != 0 {
		return nil, errors.New("cannot safely edit a flow-style config; use a block mapping")
	}
	index := len(old.Content)
	for i := 0; i < len(old.Content); i += 2 {
		if old.Content[i].Column != 1 {
			return nil, errors.New("cannot safely edit config with indented or explicit top-level keys")
		}
		if old.Content[i].Value == "workflow" {
			index = i
		}
	}

	lines := strings.SplitAfter(string(expected), "\n")
	end := len(lines)
	for i, line := range lines {
		text := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(text, "...") && (len(text) == 3 || text[3] == ' ' || text[3] == '\t') {
			end = i
			break
		}
	}
	start := end
	if index < len(old.Content) {
		start = old.Content[index].Line - 1
		if index+2 < len(old.Content) {
			end = old.Content[index+2].Line - 1
		}
		if start < 0 || start >= end || end > len(lines) {
			return nil, errors.New("cannot locate the existing workflow block safely")
		}
		// Keep separators and standalone comments before the next key or document end.
		tail := end
		indentedComment := false
		for tail > start+1 {
			line := lines[tail-1]
			comment := strings.HasPrefix(strings.TrimLeft(line, " \t"), "#")
			if !comment && strings.TrimSpace(line) != "" {
				break
			}
			indentedComment = indentedComment || comment && !strings.HasPrefix(line, "#")
			tail--
		}
		if indentedComment {
			// An indented '#' might be block scalar content rather than a comment.
			withoutTail, err := workflowMapping([]byte(strings.Join(lines[:tail], "") + strings.Join(lines[end:], "")))
			if err != nil || !sameWorkflowSplice(old, withoutTail) {
				return nil, errors.New("cannot safely distinguish trailing workflow comments from scalar content")
			}
		}
		end = tail
	}

	// Do not import the draft's document wrapper or preview comments.
	draft.Content[0] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "workflow"}
	draft.Style = 0
	var fragment bytes.Buffer
	enc := yaml.NewEncoder(&fragment)
	enc.SetIndent(2)
	if err := enc.Encode(draft); err != nil {
		return nil, fmt.Errorf("encode workflow: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	eol := "\n"
	if strings.Contains(string(expected), "\r\n") {
		eol = "\r\n"
	}
	prefix := strings.Join(lines[:start], "")
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += eol
	}
	merged := []byte(prefix + strings.ReplaceAll(fragment.String(), "\n", eol) + strings.Join(lines[end:], ""))

	// Compare the parsed splice to the intended tree, including alias targets. This catches
	// ambiguous source boundaries and aliases that would silently rebind to a draft anchor.
	want := append([]*yaml.Node(nil), old.Content[:index]...)
	want = append(want, draft.Content...)
	if index < len(old.Content) {
		want = append(want, old.Content[index+2:]...)
	}
	intended := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: want}
	parsed, err := workflowMapping(merged)
	if err != nil {
		return nil, fmt.Errorf("read merged config: %w", err)
	}
	if !sameWorkflowSplice(intended, parsed) {
		return nil, errors.New("cannot merge workflow safely: ambiguous YAML or incompatible anchors")
	}
	return merged, nil
}

// workflowMapping reads one document with unambiguous keys and anchor declarations.
func workflowMapping(data []byte) (*yaml.Node, error) {
	text := string(data)
	if !utf8.Valid(data) || strings.ContainsAny(strings.ReplaceAll(text, "\r\n", ""), "\r\x00\u0085\u2028\u2029\uFEFF") {
		return nil, errors.New("unsupported YAML encoding or line separators")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err == io.EOF {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	} else if err != nil {
		return nil, err
	}
	if err := dec.Decode(new(yaml.Node)); err != io.EOF {
		return nil, errors.New("expected a single YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || root.ShortTag() != "!!map" || root.Anchor != "" {
		return nil, errors.New("expected a plain top-level YAML mapping")
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" || key.Anchor != "" {
			return nil, fmt.Errorf("unsupported top-level YAML key at line %d (expected an unanchored string)", key.Line)
		}
	}
	anchors := make(map[string]bool)
	var check func(*yaml.Node) error
	check = func(n *yaml.Node) error {
		if n.Anchor != "" {
			if anchors[n.Anchor] {
				return fmt.Errorf("duplicate YAML anchor %q", n.Anchor)
			}
			anchors[n.Anchor] = true
		}
		if n.Kind == yaml.MappingNode {
			keys := make(map[string]bool)
			for i := 0; i < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || keys[key.Value] {
					return fmt.Errorf("unsupported or duplicate YAML key at line %d", key.Line)
				}
				keys[key.Value] = true
			}
		}
		for _, child := range n.Content {
			if err := check(child); err != nil {
				return err
			}
		}
		return nil
	}
	return root, check(root)
}

func sameWorkflowSplice(want, got *yaml.Node) bool {
	paired := make(map[*yaml.Node]*yaml.Node)
	var compare func(*yaml.Node, *yaml.Node) bool
	compare = func(want, got *yaml.Node) bool {
		if want.Kind != got.Kind || want.ShortTag() != got.ShortTag() || want.Value != got.Value ||
			want.Anchor != got.Anchor || len(want.Content) != len(got.Content) {
			return false
		}
		paired[want] = got
		if want.Kind == yaml.AliasNode {
			return want.Alias != nil && paired[want.Alias] == got.Alias
		}
		for i := range want.Content {
			if !compare(want.Content[i], got.Content[i]) {
				return false
			}
		}
		return true
	}
	return compare(want, got)
}
