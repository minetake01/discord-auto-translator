# Discord Thread and Message Sync Notes

This document records the thread/message behavior that the translator bot relies on.
It is based on the official `discord/discord-api-docs` mirror at commit
`0833444bd8aca2dde4cb899e6643322cc014cf79`, synced at
`2026-05-23T10:34:36.842Z`, plus observed behavior from this project.

Local tests are demos and regression guards for this implementation. They do not
guarantee Discord production behavior by themselves; when Discord semantics are in
question, prefer the official docs and a small real-guild verification.

## Pattern Matrix

| Pattern | Discord API behavior | Required API calls | Links to persist | Implementation notes |
|---|---|---|---|---|
| Normal parent-channel message | `MESSAGE_CREATE` with `type=DEFAULT` in a registered parent channel. | Execute the target parent channel webhook with `wait=true`. | `source parent message -> target parent message`. | This is the basic mirroring path. Store author/content snapshot for later quote/reply rendering. |
| Thread from an existing message in text/news channel | `POST /channels/{channel.id}/messages/{message.id}/threads`. Discord creates a thread whose ID is the source message ID. It emits `THREAD_CREATE` and a `MESSAGE_UPDATE` for the source message. | If the target parent message is already translated, call `POST /channels/{target_parent}/messages/{target_message}/threads`. If the target parent message is not linked yet, defer until the link exists. | `source thread/message id -> target thread id`. The parent message link remains the starter content. | Do not create a standalone target thread when the source thread is known to be message-backed but the target message link is missing. That creates the duplicate-thread bug. |
| `THREAD_STARTER_MESSAGE` for a thread from existing message | `MESSAGE_CREATE type=21` inside the new thread. It contains only a reference to the parent message; it is not real message content to translate. | No webhook send for the starter event itself. Use it as a chance to create the target thread from the referenced translated parent message if `THREAD_CREATE` arrived too early. | Reuse the parent message link and save the thread link. | Skipping this event is correct. Treating "skip" as "drop starter content" is wrong; the starter content is the translated parent message. |
| Thread without a source message | `POST /channels/{channel.id}/threads` emits `THREAD_CREATE`. There is no parent message to bind to. | Call `POST /channels/{target_parent}/threads` for non-forum/media targets. | `source thread id -> target thread id`. | Never call create-thread-from-message for this pattern. |
| Normal message inside an existing thread | `MESSAGE_CREATE` has `channel_id = source_thread_id`. | Execute the target parent webhook with `thread_id={target_thread_id}` and `wait=true`. | `source thread message -> target thread message`. | Webhook edit/delete/get also need `thread_id` for webhook messages inside threads. |
| Forum/media new post | `POST /channels/{forum_or_media}/threads` creates the thread and the first message together. The returned channel includes the created post/thread information. | For forum/media targets, call the same thread endpoint with translated title and translated first message content. | `source post thread -> target post thread` and `source first message -> target first message`. | The first message is not just the title. Passing the title as content loses the translated body. |
| Forum/media source post to a text/news target | Source creates a thread and first message together, but a text/news target cannot include the first message in thread creation. | Create the target thread, then execute the target webhook with `thread_id={target_thread_id}` for the translated first message. | `source post thread -> target thread` and `source first message -> target thread message`. | This avoids dropping the first post body when channel types differ within a translation group. |
| Additional message in forum/media post thread | Webhooks posted to forum/media channels require `thread_id` or `thread_name`; existing threads must use `thread_id`. | Execute webhook with `thread_id={target_thread_id}`. | `source post reply -> target post reply`. | Do not send both `thread_id` and `thread_name`. |
| Thread rename | `THREAD_UPDATE` carries the thread channel. | `PATCH /channels/{target_thread_id}` with translated name. | Existing thread links only. | Duplicate or no-op gateway updates should be harmless. |
| Thread delete | `THREAD_DELETE` carries the thread channel. | `DELETE /channels/{target_thread_id}`. | Delete source/target thread links. | Deleting Discord channels is irreversible; keep this path narrow. |
| Message edit/delete/reaction/pin in threads | The message belongs to the thread channel. Webhook messages in threads need `thread_id` for edit/delete. | Edit/delete webhook message with `thread_id`. Reaction/pin APIs use the target message's actual channel ID. | Existing message links. | If the link target channel is a target thread, resolve the parent registered channel only for webhook credentials. |

## Current Implementation Rules

- `THREAD_CREATE` from the gateway uses `SyncThreadCreateFromGateway`, which defers message-backed thread creation when the translated parent message link is not available yet.
- `MESSAGE_CREATE` for `THREAD_STARTER_MESSAGE` never sends a translated webhook message, but it can complete the deferred thread sync by using `ReferencedMessageID`.
- Thread creation is serialized inside `Service` with a mutex and remains guarded by stored thread links. This reduces duplicate target threads when gateway events arrive in different orders.
- `CreateThread` returns both the target thread ID and, for forum/media targets, the first message ID. The service stores both the thread link and the first-message link when available.
- For forum/media source posts targeting non-forum channels, the service creates the target thread first and then sends the translated first message through the webhook into that target thread.
- For messages inside target threads, webhook send/edit/delete must carry the target thread ID while still using the parent registered channel's webhook credentials.

## Testing Guidance

Keep local tests focused on expected API calls and persisted links:

- `THREAD_CREATE -> THREAD_STARTER_MESSAGE` when the parent message link appears later.
- First normal thread message arriving before or after `THREAD_CREATE`.
- Repeated `SyncThreadCreate` / `SyncThreadCreateFromGateway` calls creating only one target thread per group and target parent.
- Existing-message-backed threads using `CreateThreadFromMessage` only when the translated target parent message exists.
- Forum/media posts preserving translated first-message content and saving the first-message link.
- Webhook edit/delete in target threads passing `thread_id`.

When changing Discord behavior assumptions, verify against a real test guild and log
the incoming gateway event type, channel ID, parent ID, message ID, referenced
message ID, and webhook/thread IDs.
