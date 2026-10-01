# DESIGN.md — wtui TUI Design System

Extracted from the existing Bubble Tea / Lipgloss implementation. This is the visual
contract: every future style must map to these tokens and patterns. Source of truth:
`internal/tui/theme/palette.go`, `internal/tui/panels/`, `internal/tui/header.go`,
`internal/tui/layout.go`, `internal/tui/footer.go`, `internal/tui/styles.go`.

## 1. Atmosphere & Identity

- Product: `wtui` — one task, many repositories, one terminal.
- Users: developers managing task-scoped git worktrees across multi-repo services,
  including keyboard-only users and users on CJK terminals.
- Tone: operational, dense, dark. Information beats decoration. No gradients,
  no animation for animation's sake.
- Brand: `◉ wtui` mark (Info dot, Primary bold wordmark) with
  `│ git worktree manager` descriptor in TextMuted, inside the header card.

## 2. Color

Palette is fixed in `internal/tui/theme/palette.go`. Never invent new values.

| Token | Hex | Role |
|---|---|---|
| `Background` | `#090E1A` | terminal background |
| `Surface` | `#0D1424` | base panel surface |
| `SurfaceRaised` | `#111A2D` | raised surface (cards, dialogs) |
| `Border` | `#50617A` | neutral borders |
| `BorderStrong` | `#8DAECC` | emphasized borders (neutral workflow cards) |
| `Primary` | `#C5A6FF` | focus, selection, active/current state |
| `PrimaryMuted` | `#272140` | muted primary fill |
| `Text` | `#F1F6FF` | primary text, keys in footer hints |
| `TextMuted` | `#A8B5CA` | dim/secondary text, separators, hints |
| `Success` | `#68E6AE` | done/clean/idle-OK |
| `Warning` | `#FFD27A` | modified, in-progress |
| `Danger` | `#FF8299` | blocked, failed, stale, behind |
| `Info` | `#75D8FF` | informational states (prepared, syncing) |
| `GlassHighlight` | `#D7ECFF` | top/left glass border edge |
| `GlassShadow` | `#6B7D99` | bottom/right glass border edge |

Semantic mapping is mandatory: `done`→Success, `now`→Primary, `blocked`/`failed`→Danger,
`next`/dim→TextMuted, neutral text→Text. Release statuses map via
`releaseStatusColor` (released/masterMerged→Success, in-progress→Warning,
prepared/syncingDevelop→Info, awaitingMasterMerge→Primary, draft/rejected→TextMuted).

## 3. Typography

- System monospace only (the terminal's font). No font loading, no custom families.
- Two weights: default and `Bold(true)`. Bold marks titles, current/active state,
  blockers, and counters.
- No size variation — one cell height. Hierarchy comes from weight and color, never size.
- Ellipsis for truncation: `…` via `ansi.Truncate`.

## 4. Spacing & Layout

- Unit is the terminal cell. All spacing is integer cell counts.
- Layout tiers by terminal width: **narrow <80**, **compact 80–119**, **wide ≥120**.
  - Narrow: single-column stack, 1 vertical gutter, output height capped at 4.
  - Compact/wide: tasks column 34% (compact) / 29% (wide), remainder right column.
- Header height ≤3 (1 when terminal height <12). Footer height 1.
- Borders consume 2 cells per axis: inner content = `width-2`, `height-2`
  (`innerDimensions`).
- Standard padding: `Padding(0, 1)` inside cards; footer `PaddingLeft/Right(1)`.
- Inline separation: two spaces between chips/segments; ` ─▶ ` between chain steps;
  ` → ` between workflow cards.
- All truncation/wrapping must be ANSI- and CJK-aware: use `ansi.Truncate`,
  `ansi.Wrap`, `lipgloss.Width`. Never byte-slice styled strings.

## 5. Components

### Glass card (base primitive)

- Unfocused: `theme.GlassBorder(GlassHighlight)` — rounded border, top/left in the
  highlight color, bottom/right in GlassShadow.
- Focused: `theme.FocusedGlassBorder(Primary)` — thick border in Primary.
- Selected (non-focus) cards: keep rounded glass border, override top/left edges to
  Primary, plus a `▌` selection rail on the left edge of the first line.
- Panels use `panelBorderStyle(focused)`; cards inside lists use the rail + edge
  recolor pattern.

### Header

GlassBorder card, full width minus 2, `Padding(0, 1)`. Contents: brand, descriptor,
wide-tier-only chips (`repo:`, `branch:`, `cwd:`), version (hidden on narrow),
status indicator (`●` Success idle, `◉` Primary busy). Truncated with `…`.

### Pane title

Bold Primary left title, dim right peer-tab hint, space-filled gap; truncates to
left title when tight (`renderPaneTitle`).

### Service card

Rail + `▣` + bold name (Primary when selected) + state icon/text (`✓ clean` Success,
`⚠ modified` Warning, `✗ STALE` Danger) + workflow badge + branch (`⎇`, Primary) +
`↑n` Success / `↓n` Danger counters. Line 2: operation progress marker then `Path:`.
Four cells tall, 1 cell spacing.

### Footer

TextMuted labels with Text keys, `[k] action` segments joined by two spaces. Drops
hints from the middle when overflowing; collapses to essential set below width 100.
Spinner prepended while an operation runs.

### Markers (shared vocabulary)

- Steps: `✓` done, `●` now, `✗` blocked, `○` next/pending.
- Phase icons: `</>` code, `⎇` MR/merge, `⟳` review/CI/regression, `◆` tag, `◇` default.
- Progress cells: `□` pending, `■` colored by state.
- Every colored state also carries a distinct glyph — color is never the only signal.

### Workflow Panel (new, non-focusable strip under the header)

A compact, read-only strip rendered directly beneath the header, above the main
columns. It summarizes the selected task or release without ever taking focus.

- **Placement:** full-width row between header and the tasks/services/releases
  columns. Height is content-derived (chain/cards + one message line), deducted from
  main area height like the header is.
- **Identity line:** task ID (bold Text) or release ID + version (`vX.Y`, or
  `Versions: mixed`), in the pane-title style — bold Primary label, dim context.
- **Steps:** reuse the existing workflow renderer. Wide (≥90 cells and fits): step cards
  (`workflowCardStyle` — glass border per state, icon + marker + label, joined by
  dim `→`). Otherwise the one-line chain (`marker label ─▶ …`). Falls back to
  wrapped chain rows via `workflowRowEnd` when the chain exceeds width.
- **Task-level blocker/action:** one line below the steps. Blocker:
  `ⓘ <text>` bold Danger. Next action: `ⓘ <text>` TextMuted. Wrapped with
  `ansi.Wrap`, never truncated mid-word.
- **Selected-service guidance:** when a service is selected, its workflow detail
  renders as dim `  <service>: <detail>` lines (same pattern as the help overlay),
  Danger-tinted when the service is `blocked`/`failed`.
- **Loading state:** while workflow data is unresolved, render a single dim line
  (`Loading workflow…`-equivalent text, TextMuted). No spinner in the strip — the
  header indicator and footer already signal activity.
- **Empty state:** no task/release selected → render nothing (zero height). Task
  with no workflow → nothing. The strip must not reserve blank lines.
- **Blocked state:** blocker line in bold Danger with `ⓘ`; affected step shows
  `✗` in Danger; blocked card gets a Danger glass border. Steps after the blocker
  stay `○` TextMuted.
- **Narrow-terminal behavior (<80):** strip renders chain-only, wrapped to rows by
  width, cards suppressed (mirrors the existing `<90` path in `renderWorkflow`).
  Identity line truncates with `…`. If height is scarce (terminal height <12,
  where the header already collapses to 1 line), the strip collapses to the
  blocker/action line only.
- **Focus:** the panel is NOT focusable. No border-state change, no keybindings, no
  cursor. It always renders in the unfocused glass treatment — or borderless when a
  border would cost two rows the main panes need.

## 6. Motion & Interaction

- Motion is limited to the operation spinner in the footer and Bubble Tea's built-in
  list scrolling. No transitions, no decorative animation.
- Interaction is message-driven: state changes re-render; there is no per-frame
  animation loop for visuals.
- Focus changes are instant border swaps (rounded→thick, dim→Primary). Selection
  moves instantly. Nothing eases.

## 7. Depth & Surface

- Depth comes from the glass border alone: bright top/left edge, shadowed
  bottom/right edge over the dark Background/Surface colors.
- Three levels: terminal Background → panel Surface (glass border) → raised cards
  (SurfaceRaised, per-state border colors).
- No blur, no translucency, no box-shadow effects beyond the two-tone border.
- The Workflow Panel sits at panel level, one step below the header in emphasis
  (dimmer text, no focus treatment).

## 8. Accessibility & Debt

**Accessibility (existing guarantees to preserve):**

- Keyboard-only operation: every action has a keybinding; footer and `?` help overlay
  document them per context.
- Semantic markers plus color: every colored state has a glyph (`✓ ● ✗ ○ ⚠ ▌ ▣ ⓘ`),
  so state survives monochrome or color-blind rendering.
- ANSI/CJK-aware measurement everywhere (`lipgloss.Width`, `ansi.Truncate`,
  `ansi.Wrap`) so wide glyphs do not break layout.
- True-color terminal recommended; palette is readable on the dark background at
  default terminal settings.

**Accepted debt:**

- No reduced-motion setting — motion is already minimal (spinner only).
- No high-contrast or light theme; single dark palette.
- Workflow appears only in this strip. Services and releases panes retain their own
  list/detail content but do not duplicate task/release workflow guidance.
- `workflowRowEnd` measures chain width from unstyled text; acceptable because
  markers are single-cell and labels are plain.
