package modal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type metadataField struct {
	key, label, value string
}

// MetadataEditor keeps generated text in the same review dialog until accepted.
type MetadataEditor struct {
	fields          []metadataField
	editor          textarea.Model
	editing         bool
	selected        int
	generating      bool
	generationError string
	width           int
	compact         bool
	dirty           bool
	discarding      bool
	editorHeight    int
	contentHeight   int
}

func (e *MetadataEditor) SetMetadataStatus(loading bool, message string) {
	e.generating, e.generationError = loading, message
}

func (e *MetadataEditor) setMetadataField(key, label, value string) {
	for i := range e.fields {
		if e.fields[i].key == key {
			e.fields[i].value = value
			return
		}
	}
	e.fields = append(e.fields, metadataField{key, label, value})
}

func (e *MetadataEditor) metadataValue(key string) string {
	if e.editing && e.fields[e.selected].key == key {
		return e.editor.Value()
	}
	for _, field := range e.fields {
		if field.key == key {
			return field.value
		}
	}
	return ""
}

func (e *MetadataEditor) applyMetadata(draft ai.Draft) {
	e.setMetadataField("title", "Review title", draft.Title)
	e.setMetadataField("description", "Review description", draft.Description)
	e.fields = slices.DeleteFunc(e.fields, func(field metadataField) bool {
		return strings.HasPrefix(field.key, "reason:")
	})
	e.selected = min(e.selected, len(e.fields)-1)
	e.dirty = true
}

func (e *MetadataEditor) confirmMetadataDiscard() bool {
	e.discarding = e.dirty
	return e.discarding
}

func (e *MetadataEditor) metadataInputView(input textinput.Model) string {
	if e.generating {
		input.Blur()
	}
	return input.View()
}

func (e *MetadataEditor) metadataTags() map[string]string {
	result := make(map[string]string)
	for _, f := range e.fields {
		if strings.HasPrefix(f.key, "tag:") {
			result[strings.TrimPrefix(f.key, "tag:")] = e.metadataValue(f.key)
		}
	}
	return result
}

func (e *MetadataEditor) metadataVersions() map[string]string {
	result := make(map[string]string)
	for _, f := range e.fields {
		if strings.HasPrefix(f.key, "version:") {
			result[strings.TrimPrefix(f.key, "version:")] = e.metadataValue(f.key)
		}
	}
	return result
}

func (e *MetadataEditor) metadataSize(width, height int) {
	contentWidth, contentHeight := overlayContentSize(width, height)
	e.contentHeight = contentHeight
	e.width = max(24, contentWidth)
	e.compact = contentHeight < 18
	e.editorHeight = max(1, min(5, contentHeight-9))
	if e.editing {
		e.editor.SetWidth(e.width)
		e.editor.SetHeight(e.editorHeight)
	}
}

func (e *MetadataEditor) updateMetadata(msg tea.Msg) (bool, tea.Cmd) {
	if e.discarding {
		if !e.dirty {
			return true, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y":
				e.dirty = false
				return true, func() tea.Msg { return CloseModalMsg{} }
			case "esc", "n":
				e.discarding = false
			}
		}
		return true, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+e" && !e.editing {
		if len(e.fields) == 0 {
			e.setMetadataField("title", "Review title", "")
			e.setMetadataField("description", "Review description", "")
		}
		e.editor = textarea.New()
		e.editor.ShowLineNumbers = false
		e.editor.CharLimit = 0
		e.editor.SetWidth(max(24, e.width))
		e.editor.SetHeight(max(1, e.editorHeight))
		e.selected = 0
		e.editor.SetValue(e.fields[0].value)
		e.editing = true
		return true, e.editor.Focus()
	}
	if !e.editing {
		return false, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+s", "esc":
			e.fields[e.selected].value = e.editor.Value()
			e.editing = false
			e.editor.Blur()
			return true, nil
		case "tab", "shift+tab":
			e.fields[e.selected].value = e.editor.Value()
			delta := 1
			if key.String() == "shift+tab" {
				delta = -1
			}
			e.selected = (e.selected + delta + len(e.fields)) % len(e.fields)
			e.editor.SetValue(e.fields[e.selected].value)
			return true, nil
		case "ctrl+u":
			e.editor.SetValue("")
			return true, nil
		}
	}
	previous := e.editor.Value()
	var cmd tea.Cmd
	e.editor, cmd = e.editor.Update(msg)
	e.dirty = e.dirty || previous != e.editor.Value()
	return true, cmd
}

func (e *MetadataEditor) MetadataView() string {
	if e.discarding {
		return lipgloss.NewStyle().Foreground(modalColorWarning).Render(ansi.Wrap(
			"Discard draft and close?\nGenerated and edited metadata will be lost.\n[y] discard  [Esc/n] keep editing", max(24, e.width), " "))
	}
	var b strings.Builder
	if e.compact && !e.editing {
		status := "Ctrl+E edits metadata"
		if e.generating {
			status = "AI generating; Esc cancels"
		}
		if e.generationError != "" {
			status = "AI: " + e.generationError
		}
		if e.generationError != "" {
			status = lipgloss.NewStyle().Foreground(modalColorDanger).Render(ansi.Truncate(e.generationError, e.width, "..."))
		}
		return status + "\nCtrl+G sends commits to AI"
	}
	if e.editing {
		editor := e.editor
		if e.generating {
			editor.Blur()
		}
		b.WriteString(fmt.Sprintf("%s (%d/%d)\n%s", e.fields[e.selected].label, e.selected+1, len(e.fields), editor.View()))
		if e.generating {
			b.WriteString("\nInputs paused while AI generates\n")
		} else {
			b.WriteString("\nTab: field  Ctrl+S: apply  Ctrl+U: clear\nEsc: back, keep draft\n")
		}
	} else if len(e.fields) > 0 {
		reasonShown := false
		tagShown := false
		for _, f := range e.fields {
			isReason := strings.HasPrefix(f.key, "reason:") && !reasonShown
			isTag := strings.HasPrefix(f.key, "tag:") && !tagShown
			if f.value != "" && (f.key == "title" || f.key == "description" || isReason || isTag) {
				value := f.label + ": " + strings.ReplaceAll(f.value, "\n", " / ")
				b.WriteString(ansi.Truncate(value, max(24, e.width), "...") + "\n")
				reasonShown = reasonShown || isReason
				tagShown = tagShown || isTag
			}
		}
	}
	if e.generating {
		b.WriteString("Generating with AI... Esc cancels\n")
	} else if e.generationError != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(modalColorDanger).Render(e.generationError) + "\n")
	}
	b.WriteString("Ctrl+G sends committed changes to\nconfigured AI provider.\nCtrl+E edits metadata before confirmation.")
	width := max(24, e.width)
	return lipgloss.NewStyle().Foreground(modalColorDim).Render(ansi.Wrap(b.String(), width, " "))
}
