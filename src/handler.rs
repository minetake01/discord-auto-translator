mod ready;
mod cache_ready;
mod guild_delete;
mod channel_delete;
mod thread_create;
mod shard_ready;

use poise::{serenity_prelude::{Context, FullEvent}, FrameworkContext};

use crate::{Data, AppError};

use self::{
    ready::ready,
    cache_ready::cache_ready,
    shard_ready::shard_ready,
    guild_delete::guild_delete,
    channel_delete::channel_delete,
    thread_create::thread_create,
};

pub async fn event_handler<'a>(
    ctx: &'a Context,
    event: &'a FullEvent,
    _framework: FrameworkContext<'a, Data, AppError>,
    data: &'a Data
) -> Result<(), AppError> {
    match event {
        FullEvent::Ready { data_about_bot } => ready(ctx, data_about_bot),
        FullEvent::CacheReady { guilds } => cache_ready(guilds),
        FullEvent::ShardsReady { total_shards } => shard_ready(total_shards),
        FullEvent::GuildDelete { incomplete, full } => guild_delete(data, incomplete, full).await,
        FullEvent::ChannelDelete { channel, messages: _ } => channel_delete(data, channel).await,
        FullEvent::ChannelPinsUpdate { pin } => todo!("ChannelPinsUpdate"),
        FullEvent::ThreadCreate { thread } => thread_create(ctx, data, thread).await,
        FullEvent::ThreadUpdate { old, new } => todo!("ThreadUpdate"),
        FullEvent::ThreadDelete { thread, full_thread_data } => todo!("ThreadDelete"),
        FullEvent::Message { new_message } => todo!("Message"),
        FullEvent::MessageUpdate { old_if_available, new, event } => todo!("MessageUpdate"),
        FullEvent::MessageDelete { channel_id, deleted_message_id, guild_id } => todo!("MessageDelete"),
        FullEvent::MessageDeleteBulk { channel_id, multiple_deleted_messages_ids, guild_id } => todo!("MessageDeleteBulk"),
        FullEvent::ReactionAdd { add_reaction } => todo!("ReactionAdd"),
        FullEvent::ReactionRemove { removed_reaction } => todo!("ReactionRemove"),
        FullEvent::ReactionRemoveAll { channel_id, removed_from_message_id } => todo!("ReactionRemoveAll"),
        event => {
            #[cfg(debug_assertions)]
            println!("Unhandled Event: {}", event.snake_case_name());
            Ok(())
        },
    }
}
