---
name: discord-api-docs
description: Look up the current official Discord Developer API documentation from discord/discord-api-docs with minimal context. Use when answering questions or implementing code involving Discord REST endpoints, Gateway events/opcodes/intents, interactions, slash commands, application commands, webhooks, OAuth2, permissions, rate limits, components, activities, or Discord Social SDK docs.
---

# Discord API Docs

Use the official `discord/discord-api-docs` repository as the source of truth.
Keep the local mirror current before relying on cached content.

## Workflow

1. Sync the docs at the start of each Discord API task unless the user
   explicitly asks for offline work:
   ```sh
   deno run --allow-read --allow-write --allow-run --allow-env scripts/sync_docs.ts
   ```
2. Search for the narrowest relevant pages:
   ```sh
   deno run --allow-read scripts/find_docs.ts "interaction callback message flags"
   ```
3. Read only the returned files or line windows from the mirrored repo. Prefer
   `rg` within the mirror when a symbol, endpoint path, opcode, event name, or
   field name is known.
4. Cite the docs file path and commit from `references/state.json` when giving
   version-sensitive answers.

## Local Data

- The mirror defaults to `references/discord-api-docs-repo`.
- `references/doc-index.jsonl` is generated from `.md` and `.mdx` files.
- `references/state.json` records the upstream URL, branch, commit SHA, and sync
  time.
- Override the mirror location with `DISCORD_API_DOCS_REPO` if a shared clone
  already exists.

## Search Tactics

- Query endpoint paths literally, for example `/channels/{channel.id}/messages`.
- Query Discord object names with nearby terms, for example
  `interaction callback type`, `gateway intent guild messages`, or
  `application command permissions`.
- Use `find_docs.ts --json` when another script or agent needs structured
  results.
- After search, load the few matching source files rather than loading generated
  indexes into context.

## Repository Map

- `developers/` contains the current Developer Docs content, including API
  reference and guides.
- `resources/discord-social-sdk/` contains Discord Social SDK documentation.
- `snippets/` contains reusable examples included by docs pages.
- `docs.json` is the Mintlify navigation/config entry point and helps locate
  renamed or reorganized pages.

## Freshness Rules

- Treat cached docs as stale until `sync_docs.ts` succeeds for the current task.
- If network access is unavailable, use the existing mirror and state the cached
  commit SHA and sync time.
- Do not rely on memory for Discord API details that may change; search the
  synced docs first.
