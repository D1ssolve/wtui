package forge

import (
	"encoding/json"
	"errors"
	"fmt"
)

// GitLab's global "id" is not a project-local MR number.
type glabMRIdentity struct {
	IID    json.RawMessage `json:"iid"`
	Number json.RawMessage `json:"number"`
}

func (id glabMRIdentity) localNumber() (int, error) {
	number := 0
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{{"iid", id.IID}, {"number", id.Number}} {
		if len(field.raw) == 0 {
			continue
		}
		var value int
		if err := json.Unmarshal(field.raw, &value); err != nil {
			return 0, fmt.Errorf("decode MR %s: %w", field.name, err)
		}
		if value <= 0 {
			return 0, fmt.Errorf("MR %s must be a positive integer", field.name)
		}
		if number != 0 && number != value {
			return 0, fmt.Errorf("MR iid !%d conflicts with number !%d", number, value)
		}
		number = value
	}
	if number == 0 {
		return 0, errors.New("MR response has neither iid nor number")
	}
	return number, nil
}
