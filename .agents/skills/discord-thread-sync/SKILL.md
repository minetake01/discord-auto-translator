---
name: discord-thread-sync
description: >-
  Discord thread, forum, and media-channel mirroring semantics for this bot
  (THREAD_CREATE ordering, THREAD_STARTER_MESSAGE, defer-until-parent-link,
  webhook thread_id, reverse-link skip). Use when changing thread or forum
  sync, message mirroring inside threads, forum tags, or webhook send/edit/delete
  in threads.
---

# Discord thread and message sync

Official Discord API docs are the source of truth for wire format. Use the
`discord-api-docs` skill when an endpoint or gateway event is in question.
Local tests guard this bot's mapping; they do not prove Discord production
behavior. After changing an assumption, verify on a real guild and log
gateway type, channel ID, parent ID, message ID, referenced message ID, and
webhook/thread IDs.

## Invariants

- Defer without translating when a message-backed thread's parent link is
  missing, or a forum/media `THREAD_CREATE` has no initial payload. Complete
  later via `THREAD_STARTER_MESSAGE` or the first real body.
- `THREAD_STARTER_MESSAGE` (type 21) is not content to translate. Skipping the
  webhook send is correct; the starter body is the translated parent message.
- Do not create a standalone peer thread when the source is known to be
  message-backed but the target parent message link is still missing.
- Do not create a peer if the thread ID is already stored as a source **or** a
  target. Messages in a mirrored thread follow `ThreadTargets` reverse mapping.
- Thread webhook send/edit/delete use the **parent registered channel**
  credentials plus `thread_id` of the peer thread. Do not send both `thread_id`
  and `thread_name`.
- Forum/media groups only target forum/media peers. Map `applied_tags` through
  `forum_tag_maps`; omit unmapped tags; fail the target create when the
  destination has `REQUIRE_TAG` and the mapped set is empty.

## Pattern matrix

| Pattern | Discord behavior | This bot |
|---|---|---|
| Parent-channel message | `MESSAGE_CREATE` type DEFAULT | Parent webhook `wait=true`. Persist source→target message link and content snapshot. |
| Thread from existing text/news message | `POST .../messages/{id}/threads`. Thread ID equals source message ID. Emits `THREAD_CREATE` and parent `MESSAGE_UPDATE`. | `CreateThreadFromMessage` on the translated parent when the link exists; otherwise defer. Persist thread link. Parent message link remains starter content. |
| `THREAD_STARTER_MESSAGE` | type 21, reference only | No webhook. May finish deferred create from `ReferencedMessageID`. |
| Thread with no source message | `POST .../threads` → `THREAD_CREATE` | Create on first real thread message. Never create-from-message. Gateway `THREAD_CREATE` alone does not translate or create. |
| Message inside a thread | `channel_id` is the thread | Parent webhook with `thread_id={peer}`. If the thread is already a source or target, do not create another peer. |
| Forum/media new post | Thread + first message in one create | Translate title and first body together. Map tags. Defer gateway create with no initial payload. Persist thread link and first-message link. |
| Forum/media reply | Webhook requires `thread_id` on existing threads | Same as in-thread message. |
| Rename / tag change | `THREAD_UPDATE` | Patch peer name or mapped tags. No-op when mapped tags already match. |
| Thread delete | `THREAD_DELETE` | Delete peer thread and links. Irreversible; keep the path narrow. |
| Edit/delete/reaction/pin in threads | Message lives on the thread channel | Webhook edit/delete need `thread_id`. Reaction/pin use the target message's channel ID. |

## Tests to keep

- `THREAD_CREATE` then `THREAD_STARTER_MESSAGE` when the parent link appears later (no translation on the deferred gateway event).
- First thread message before or after `THREAD_CREATE`.
- Repeated create calls yield one target thread per group and target parent.
- A message in an already-mirrored **target** thread mirrors back without a reverse thread.
- Message-backed threads call create-from-message only when the translated parent exists.
- Forum/media posts keep translated first-message content and save that link.
- Webhook edit/delete in target threads pass `thread_id`.
