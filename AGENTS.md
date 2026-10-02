# Agent instructions

## Language

English. Log messages are English in every file in this repo. When you edit a file, translate the Chinese in that file to English before you finish. The file is done when a search for Chinese characters in it is empty. Keep a Chinese string only when an external protocol requires those exact bytes, and note that on the line.

## Agent skills

### Issue tracker

GitHub issues on `mcpll/esp32-go-server` (always via the GitHub MCP server; `gh` only for issue dependencies or for PR creation). See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: root `CONTEXT.md` and `docs/adr/` when they exist. See `docs/agents/domain.md`.
