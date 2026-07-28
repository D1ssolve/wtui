# Reference UI Redesign Design

## Goal

Reproduce the supplied dark `wtui` reference as closely as a terminal interface permits while preserving the current task, service, workflow, release, output, modal, and keyboard behavior.

The result targets true-color terminals with a Nerd Font, remains readable with ordinary Unicode fonts, and degrades cleanly when the terminal is narrow.

## Scope

The redesign covers the main application chrome and the four existing panes:

- status header;
- tasks pane;
- services pane and workflow;
- releases pane;
- output pane;
- contextual key footer;
- shared colors, borders, badges, selection, and focus states.

Existing modal behavior remains unchanged in this pass. Modals inherit the new palette through shared styles where practical, but a separate modal layout redesign is out of scope. Domain behavior, commands, key bindings, forge operations, and release semantics do not change.

## Terminal Constraints

The reference uses effects that terminals cannot reproduce literally: proportional type, arbitrary line height, soft shadows, blur, gradients, and translucent surfaces. The TUI substitutes:

- terminal cells for pixel positioning;
- Unicode rounded borders for rounded rectangles;
- solid dark surfaces for gradients and transparency;
- color and weight changes for glow and shadow;
- Nerd Font glyphs where safe, with standard Unicode symbols as the baseline.

Exact rendering depends on the user's terminal font and color profile. Layout and information hierarchy, rather than pixel identity, are the compatibility contract.

## Visual System

### Palette

Use a single shared palette rather than package-local color literals:

| Token | Value | Use |
| --- | --- | --- |
| `Background` | `#090E1A` | application canvas where the terminal supports it |
| `Surface` | `#0D1424` | panels and cards |
| `SurfaceRaised` | `#111A2D` | selected rows and header chips |
| `Border` | `#202A3D` | inactive borders and dividers |
| `BorderStrong` | `#33405A` | visible structural separators |
| `Primary` | `#A675FF` | focus, active workflow, selected metadata |
| `PrimaryMuted` | `#272140` | selected-row background |
| `Text` | `#D8DEED` | primary text |
| `TextMuted` | `#7E899F` | secondary metadata |
| `Success` | `#35D07F` | clean, done, healthy |
| `Warning` | `#F2B84B` | dirty, waiting |
| `Danger` | `#F06472` | stale, failed, blocked |
| `Info` | `#62A8FF` | neutral remote/branch information |

All status colors must also have a textual or symbolic cue. Color alone never carries state.

### Borders and spacing

- Main surfaces use `lipgloss.RoundedBorder()`.
- Inactive panels use `Border`; the focused panel uses `Primary` only on its border and local selection marker.
- There is a one-cell gutter between top-level panels and rows.
- Titles use uppercase labels and compact `[current/total]` counters.
- Content has one cell of horizontal inset inside each top-level panel whenever terminal width permits.

### Responsive tiers

The layout has three deterministic tiers:

1. **Wide (`>= 120` columns):** header metadata chips, 29% tasks pane, 71% right pane, full workflow cards, two-line service cards, output, and full footer.
2. **Compact (`80–119` columns):** abbreviated header metadata, 34% tasks pane, compact workflow chain, service rows with reduced metadata, and context-prioritized footer hints.
3. **Narrow (`< 80` columns):** single-column stack with tasks above the active right pane, hidden decorative metadata, textual workflow chain, and minimal footer. No essential action or state may be clipped solely because the layout is narrow.

Height is also adaptive. Output aims for roughly 22% of usable height, respects configured output lines as a preference, and yields enough space for the main content. Extremely short terminals favor the focused pane and compact chrome.

## Header

The header is a one-panel status bar modeled on the reference:

```text
┌  ◉  wtui  │ git worktree manager    repo: wtui    branch: main      cwd: …   v0.4.0  ● ┐
```

- Left cluster: small app glyph, purple `wtui`, divider, muted product description.
- Center cluster: repository and branch chips when discoverable from application context.
- Right cluster: shortened working directory, version when provided by the composition root, and operation/idle indicator.
- Missing values are omitted; placeholder data is never fabricated.
- On compact widths, keep app name, selected task/branch context, and activity indicator in that priority order.
- On narrow widths, render only app name plus contextual task or release title.

Version is passed into the TUI through `Options`; the entrypoint supplies its already resolved build version. Repository/branch labels derive from existing task/service/config data where possible and otherwise stay absent.

## Tasks Pane

Each normal task becomes a two-line item:

```text
  ▸  ◌ IIPR-596                                      ●
     ├─ feature/IIPR-596
```

- First line: disclosure marker, task/worktree symbol, task ID, and right-aligned health indicator.
- Second line: tree connector and derived branch label.
- Selected item uses `PrimaryMuted` across the available row width, purple primary text, and a purple left selection rail.
- Clean/ready uses `Success`; active/selected uses `Primary`; stale uses `Danger`; unknown uses a hollow muted marker.
- Tree mode retains parent/release/hotfix hierarchy and pagination rules. It adopts the same two-line visual language without changing selection semantics.
- The title becomes `TASKS` with a right-aligned `[current/total]` counter.

Because `domain.Task` does not contain a branch field, the display uses the existing phase/version/task conventions (`feature/<id>`, `release/<version>`, `hotfix/<version>`). No domain DTO is introduced solely for decoration.

## Services Pane

The pane header contains the current task and the Services/Releases peer tab. Beneath it, the workflow is the dominant element.

### Workflow

On wide terminals each step is rendered as a fixed-height card joined by arrows:

```text
┌  </>  code  ┐  →  ┌  ⎇  MR  ┐  →  ┌  ⟳  review + CI  ┐  →  ┌  ⎇  merge  ┐
```

- done: green border/text;
- current: purple border/text and raised background;
- next: muted border/text;
- blocked: red border/text;
- subsequent line: blocker or next-action copy using existing `WorkflowSummary` data.

Compact and narrow modes fall back to a wrapped chain while retaining the same symbols, labels, and state colors.

### Service cards

Each service occupies a two-line bordered card:

```text
▌  ▣  paymentservice   ✓ clean   ready       ⎇ feature/IIPR-596   ↑1 ↓0
      Path: services/paymentservice
```

- selected card has a purple left rail and raised surface;
- name and cleanliness are always visible;
- workflow status is rendered as a small badge;
- branch and ahead/behind appear on wide and compact widths when space permits;
- repository path appears on the second line and is truncated from the left only when necessary to preserve the basename;
- stale services use danger styling and explicit `STALE` text;
- cards remain selectable through the existing Bubbles list model and retain all current commands.

## Releases Pane

Releases reuse the right-pane visual language rather than introducing a separate table theme:

- title and Services peer tab match the Services pane;
- workflow cards use release phases;
- release entries are bordered cards with release ID/version, status badge, creation date, and service count;
- selected-release detail lists service/version/tag/MR state below the cards;
- status colors map to the shared semantic palette;
- existing prepare, promote, merge, finalize, refresh, retry, reject, and remove behavior is unchanged.

## Output Pane

The output panel is a raised surface with an `OUTPUT` title and optional clear affordance label. Entries use:

```text
14:36:52   ✓  paymentservice is clean
```

- New entries receive a local `HH:MM:SS` timestamp when appended.
- Success, warning, and failure symbols are inferred conservatively from the existing message outcome where explicit status is available; otherwise use a neutral prompt marker.
- Existing stored text remains unchanged for behavioral assertions and logging; timestamp and symbol are presentation metadata.
- Scrolling and focus behavior remain unchanged.

No clear command is added in this redesign because it would change behavior. The header must not advertise a non-functional action.

## Footer

Footer hints become individual keycaps followed by labels:

```text
[a] add   [m] forge   [p] pipeline   [v] validate   [M] merge   [?] help   [Esc] back
```

- Keycap: raised surface, stronger border/text.
- Label: muted text.
- Available actions remain derived from the current focus and release state.
- Wide mode shows the full set; compact mode drops lowest-priority hints from the right; narrow mode guarantees help, back/quit, and the primary action.
- An active operation prepends the spinner in purple.

## Composition and Data Flow

The redesign stays within the existing message-driven architecture:

- `cmd/wtui` passes resolved version metadata through `app`/TUI options.
- `Model.View` selects a responsive layout tier and composes header, panels, output, and footer.
- `internal/tui` owns application chrome and responsive composition.
- `internal/tui/panels` owns panel and card rendering.
- `internal/domain` remains unchanged; visual view models stay private to `tui` or `panels`.
- Existing commands, messages, manager interfaces, and update paths remain intact.

A shared theme definition prevents color drift between `tui` and `panels`. To avoid an import cycle, the shared palette lives in a small lower-level `internal/tui/theme` package that both packages can import.

## Testing Strategy

Tests follow red-green-refactor and assert semantic rendering after stripping ANSI sequences:

- shared palette/styles expose consistent focus and status behavior;
- header renders available metadata and omits unavailable fields at each width tier;
- tasks render two-line items, derived branch labels, status markers, and selection without breaking tree navigation;
- workflow renders cards in wide mode and a wrapped chain in compact/narrow mode;
- service and release cards preserve essential information at width boundaries;
- output presentation includes timestamps through an injectable clock to keep tests deterministic;
- footer prioritization retains mandatory actions when space is constrained;
- model layout dimensions sum to the terminal width/height in all tiers;
- existing update and end-to-end tests remain green.

Tests avoid brittle full ANSI snapshots. They check dimensions, required content, omission rules, and selected semantic escape sequences where color/focus is itself the behavior.

## Acceptance Criteria

- At `>= 120x35`, the main screen visibly matches the reference's hierarchy: status header, task sidebar, workflow-led right pane, two-line cards, output surface, and keycap footer.
- Palette, selected rows, focus rails, status badges, and workflow states consistently use the defined tokens.
- The application remains usable at `80x24` and does not panic or produce negative dimensions below that size.
- Essential task/service/release identity and state remain visible in every responsive tier.
- Existing keyboard commands, focus cycling, dialogs, Git/forge operations, and workflow/release behavior do not regress.
- Rendering does not fabricate repo, branch, version, timestamps, or statuses unavailable from real application state.
- `gofmt`, `go vet ./...`, and `go test ./...` pass.

## Non-goals

- Pixel-perfect reproduction of font metrics, blur, glow, shadows, or transparency.
- Mouse-driven buttons or a new clear-output command.
- Domain workflow changes.
- Replacing Bubble Tea, Bubbles, or Lipgloss.
- Redesigning every modal layout in the same change.
