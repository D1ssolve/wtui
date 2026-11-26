package config

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RemoteName selects a Git config entry, never a transport URL or a path.
// Its zero value preserves the historical default for omitted configuration.
type RemoteName struct{ name string }

var remoteNamePattern = regexp.MustCompile("^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$")

func (r *RemoteName) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("forge.remote: expected a Git remote name, got %s", node.Tag)
	}
	name := node.Value
	if !remoteNamePattern.MatchString(name) || strings.Contains(name, "..") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("forge.remote: expected a Git remote name, got %q", name)
	}
	r.name = name
	return nil
}

func (r RemoteName) Explicit() bool { return r.name != "" }

func (r RemoteName) String() string {
	if r.name == "" {
		return "origin"
	}
	return r.name
}
func (r RemoteName) Tracking(branch string) string { return r.String() + "/" + branch }
func (c *Config) ServiceRemote() RemoteName {
	if c == nil || c.Forge == nil {
		return RemoteName{}
	}
	return c.Forge.Remote
}

func (r RemoteName) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// yaml.v3 skips field unmarshallers for null. Check presence before decoding the block.
func (c *ForgeConfig) UnmarshalYAML(node *yaml.Node) error {
	var fields struct {
		Remote yaml.Node `yaml:"remote"`
	}
	if err := node.Decode(&fields); err != nil {
		return err
	}
	remote := &fields.Remote
	for remote.Kind == yaml.AliasNode {
		remote = remote.Alias
	}
	if remote.ShortTag() == "!!null" && remote.Kind != 0 {
		return fmt.Errorf("forge.remote: expected a Git remote name, got null")
	}
	type plain ForgeConfig
	return node.Decode((*plain)(c))
}
