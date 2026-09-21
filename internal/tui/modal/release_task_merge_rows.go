package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

func taskMergeRowDisplayStatus(row task.ReleaseTaskMergeRow) string {
	if row.Status == "merged" {
		return "merged"
	}
	if row.Ready {
		return "ready"
	}
	return "blocked"
}

func shortTaskMergeSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

const taskMergeTableHeader = "Service | Task | MR | Source | Target | Status"

func taskMergeRowMR(row task.ReleaseTaskMergeRow) string {
	if row.MRNumber > 0 {
		return fmt.Sprintf("!%d", row.MRNumber)
	}
	return "-"
}

func taskMergeWideRowLine(row task.ReleaseTaskMergeRow) string {
	return fmt.Sprintf("%s | %s | %s | %s | %s@%s | %s",
		row.ServiceName, row.TaskID, taskMergeRowMR(row),
		shortTaskMergeSHA(row.HeadSHA), row.TargetBranch, shortTaskMergeSHA(row.TargetSHA),
		taskMergeRowDisplayStatus(row))
}

func taskMergeTableWidth(rows []task.ReleaseTaskMergeRow) int {
	width := ansi.StringWidth(taskMergeTableHeader)
	for _, row := range rows {
		width = max(width, ansi.StringWidth(taskMergeWideRowLine(row)))
	}
	return width
}

func renderTaskMergeRows(b *strings.Builder, rows []task.ReleaseTaskMergeRow, narrow bool) {
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	if !narrow {
		b.WriteString(dimStyle.Render(taskMergeTableHeader))
		b.WriteString("\n")
	}
	for _, row := range rows {
		var line string
		if narrow {
			line = fmt.Sprintf("%s/%s %s -> %s %s", row.ServiceName, row.TaskID, taskMergeRowMR(row), row.TargetBranch, taskMergeRowDisplayStatus(row))
		} else {
			line = taskMergeWideRowLine(row)
		}

		style := normalStyle
		if !row.Ready {
			style = warnStyle
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
		if len(row.Blockers) > 0 {
			b.WriteString(warnStyle.Render("  ⚠ " + strings.Join(row.Blockers, "; ")))
			b.WriteString("\n")
		}
	}
}
