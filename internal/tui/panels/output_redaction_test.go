package panels

import (
	"strings"
	"testing"
)

func TestOutputPanel_RedactsURLUserinfoBeforeDisplayAndHistory(t *testing.T) {
	t.Parallel()
	const diagnostic = "fetch rejected for 'https://fixture-user:O'OUTPUT-MARKER@host.invalid/repo'"
	const useful = "fetch rejected for 'https://***@host.invalid/repo'"
	panel := NewOutputPanel(160, 10)

	panel.AppendLine(diagnostic)

	for surface, text := range map[string]string{"display": stripAnsi(panel.View()), "history": panel.RecentText(1)} {
		if strings.Contains(text, "OUTPUT-MARKER") || strings.Contains(text, "fixture-user") {
			t.Errorf("%s contains URL credentials", surface)
		}
		if !strings.Contains(text, useful) {
			t.Errorf("%s lost the fetch diagnostic or host", surface)
		}
	}
}
