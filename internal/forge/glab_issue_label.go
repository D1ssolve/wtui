package forge

import "encoding/json"

// glab exposes label names as strings; older payloads also used label objects.
type glabIssueLabel string

func (l *glabIssueLabel) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		*l = glabIssueLabel(name)
		return nil
	}
	var label struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &label); err != nil {
		return err
	}
	if label.Name == "" {
		label.Name = label.Title
	}
	*l = glabIssueLabel(label.Name)
	return nil
}
