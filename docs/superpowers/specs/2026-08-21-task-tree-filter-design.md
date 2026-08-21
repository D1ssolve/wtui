# Task Tree Filter Design

## Goal

Make task filtering work when Git Flow enables the task tree view.

## Design

- Reuse the task panel's existing `list.Model` filter input and state.
- Accept both `/` and `f` to start filtering.
- Fuzzy-match task IDs with the same matcher used by the flat list.
- Show each matching task with its group header; hide groups with no matches.
- Keep the selected task when it remains visible, otherwise select the first match.
- Enter keeps the applied filter. Esc clears it and restores all tree rows.

## Verification

- Add focused panel tests for entering, applying, clearing, and rendering a tree filter.
- Run the panel tests and full Go test suite.

## Documentation

The existing user-facing shortcut documents `/`, which remains supported. No README change is needed unless implementation reveals another mismatch.
