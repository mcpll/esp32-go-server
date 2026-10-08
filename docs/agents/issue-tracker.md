# Issue tracker: GitHub

Issues and specs for this repo live as GitHub issues on `mcpll/esp32-go-server`. Reads and writes go through the GitHub MCP server (namespace `user-github`, via `GetDynamicTools` then `CallDynamicTool`). The only `gh` calls in this repo are the dependency calls under Blocking.

## Conventions

- **Confirm a write**: after `issue_write` or `add_issue_comment`, `issue_read` the same issue. The write is done when the body or the new comment is on the issue. If the tool result is an approval card, retry that same MCP call so the card is shown, say so in one sentence, and stop.
- **Create an issue**: `issue_write` with `method: create`, `title`, `body`, `labels`.
- **Read an issue**: `issue_read` (`method: get`, and `get_comments` / `get_labels` for comments and labels).
- **List issues**: `list_issues` with `state` and `labels` filters, or `search_issues` for keyword queries. Paginate in batches of 5-10.
- **Comment on an issue**: `add_issue_comment`.
- **Apply / remove labels**: `issue_write` with `method: update` and the full `labels` list.
- **Close**: `add_issue_comment` first, then `issue_write` with `method: update`, `state: closed`, and `state_reason`.
- **Sub-issue**: `sub_issue_write` (`method: add`) with the child's database id, not its `#number`.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external PRs as feature requests; `/triage` reads this flag.)_

When set to `yes`, PRs run through the same labels and states as issues, using the MCP PR tools:

- **Read a PR**: `pull_request_read` (`get`, `get_diff`, `get_comments`).
- **List external PRs for triage**: `list_pull_requests`, then keep only `author_association` of `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, or `NONE` (drop `OWNER`/`MEMBER`/`COLLABORATOR`).
- **Comment / label / close**: `add_issue_comment` (works on PRs), `issue_write` for labels, `update_pull_request` to close.

GitHub shares one number space across issues and PRs, so a bare `#42` may be either: try `pull_request_read`, and fall back to `issue_read`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue with `issue_write`.

## When a skill says "fetch the relevant ticket"

Run `issue_read` for the issue and its comments.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets.

- **Map**: a single issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body. `issue_write` with `labels: ["wayfinder:map"]`.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue (`sub_issue_write`). Where sub-issues aren't enabled, add the child to a task list in the map body and put `Part of #<map>` at the top of the child body. Labels: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: GitHub's **native issue dependencies**, the canonical, UI-visible representation. The MCP server has no tool for them, so this is the only place `gh` is allowed: `gh api --method POST repos/mcpll/esp32-go-server/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`, where `<blocker-db-id>` is the blocker's numeric **database id** (the `id` field from `issue_read`, _not_ the `#number` or `node_id`). GitHub reports `issue_dependencies_summary.blocked_by` (open blockers only, the live gate). Where dependencies aren't available, fall back to a `Blocked by: #<n>, #<n>` line at the top of the child body. A ticket is unblocked when every blocker is closed.
- **Frontier query**: list the map's open children (`list_issues` with `state: OPEN`, scoped to the map's sub-issues / task list), drop any with an open blocker (`issue_dependencies_summary.blocked_by > 0`, or an open issue in the `Blocked by` line) or an assignee; first in map order wins.
- **Claim**: `issue_write` with `method: update` and `assignees: [<me>]` (`get_me` returns the login), the session's first write.
- **Resolve**: `add_issue_comment` with the answer, then close with `issue_write`, then append a context pointer (gist + link) to the map's Decisions-so-far.
