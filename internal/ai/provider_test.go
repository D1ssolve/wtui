package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDraft_RejectsUntrustedMetadata(t *testing.T) {
	request := Request{Purpose: PurposeRelease, Services: []ServiceContext{{Name: "api"}}}
	valid := `{"title":"Release search","description":"Adds search.","services":[{"name":"api","bump":"minor","reason":"Adds an endpoint.","tag_description":"Search endpoint."}]}`
	tests := []struct {
		name string
		data string
	}{
		{"unknown service", strings.Replace(valid, `"name":"api"`, `"name":"other"`, 1)},
		{"missing service", `{"title":"Release","description":"Changes","services":[]}`},
		{"duplicate service", strings.Replace(valid, `}]}`, `},{"name":"api","bump":"patch","reason":"Fix","tag_description":"Fix"}]}`, 1)},
		{"invalid bump", strings.Replace(valid, `"minor"`, `"automatic"`, 1)},
		{"blank title", strings.Replace(valid, `"Release search"`, `" "`, 1)},
		{"multiline title", strings.Replace(valid, `"Release search"`, `"Release\nsearch"`, 1)},
		{"blank description", strings.Replace(valid, `"Adds search."`, `""`, 1)},
		{"blank reason", strings.Replace(valid, `"Adds an endpoint."`, `""`, 1)},
		{"blank tag message", strings.Replace(valid, `"Search endpoint."`, `""`, 1)},
		{"terminal escape in title", strings.Replace(valid, `"Release search"`, `"Release\u001b[2J"`, 1)},
		{"terminal escape in description", strings.Replace(valid, `"Adds search."`, `"Adds\u001b]52;c;data\u0007"`, 1)},
		{"terminal escape in reason", strings.Replace(valid, `"Adds an endpoint."`, `"Reason\u001b[31m"`, 1)},
		{"terminal control in tag message", strings.Replace(valid, `"Search endpoint."`, `"Search\u0085endpoint"`, 1)},
		{"model supplied version", strings.Replace(valid, `"bump":"minor"`, `"version":"99.0.0","bump":"minor"`, 1)},
		{"trailing response", valid + `{}`},
		{"invalid json", `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDraft([]byte(tt.data), request)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("ParseDraft error = %v; want ErrInvalidResponse", err)
			}
		})
	}
}

func TestParseDraft_ReturnsEditableMetadata(t *testing.T) {
	request := Request{Purpose: PurposeMR, Services: []ServiceContext{{Name: "api"}}}
	data := []byte(`{"title":" Add search ","description":"Adds search.\n\nTested manually.","services":[{"name":"api","bump":"minor","reason":"Adds an endpoint.","tag_description":"Search endpoint."}]}`)

	draft, err := ParseDraft(data, request)

	if err != nil {
		t.Fatal(err)
	}
	if draft.Title != "Add search" || draft.Description != "Adds search.\n\nTested manually." {
		t.Fatalf("metadata = %#v", draft)
	}
	if len(draft.Services) != 1 || draft.Services[0].Name != "api" || draft.Services[0].Bump != BumpMinor || draft.Services[0].Version != "" {
		t.Fatalf("service metadata = %#v", draft.Services)
	}
}

func TestParseDraft_NormalizesPortableWhitespace(t *testing.T) {
	request := Request{Purpose: PurposeMR, Services: []ServiceContext{{Name: "api"}}}
	data := []byte(`{"title":"Search","description":"Notes\r\n\tIndented","services":[{"name":"api","bump":"minor","reason":"New endpoint","tag_description":"Search\r\n\tEndpoint"}]}`)

	draft, err := ParseDraft(data, request)

	if err != nil {
		t.Fatal(err)
	}
	if draft.Description != "Notes\n    Indented" || draft.Services[0].TagDescription != "Search\n    Endpoint" {
		t.Fatalf("metadata retains terminal whitespace controls: %#v", draft)
	}
}
