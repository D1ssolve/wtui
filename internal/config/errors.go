// Package config provides YAML-based configuration loading and management.
// This file is the canonical location for config-package sentinel errors.
package config

import "errors"

// ErrUnknownKey is returned by SetKey when the provided key is not a recognised
// Config struct field YAML tag.
var ErrUnknownKey = errors.New("unknown config key")
