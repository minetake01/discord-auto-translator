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
        FullEvent::GuildDelete { incomplete, full } => guild_delete(ctx, data, incomplete, full).await,
        FullEvent::ChannelDelete { channel, messages } => channel_delete(ctx, data, channel, messages).await,
        FullEvent::ThreadCreate { thread } => thread_create(ctx, data, thread).await,
        FullEvent::Message { new_message } => {
            println!("Message: {}", new_message.content);
            Ok(())
        },
        event => {
            #[cfg(debug_assertions)]
            println!("Unhandled Event: {}", event.snake_case_name());
            Ok(())
        },
    }
}
