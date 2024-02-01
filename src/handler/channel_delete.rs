use poise::serenity_prelude::{Context, GuildChannel, Message};
use sea_orm::EntityTrait;

use crate::{AppError, Data, entities::channel::Entity as ChannelEntity};

pub async fn channel_delete(_ctx: &Context, data: &Data, channel: &GuildChannel, _messages: &Option<Vec<Message>>) -> Result<(), AppError> {
    ChannelEntity::delete_by_id(channel.id)
        .exec(&data.db)
        .await?;

    dbg!("Channel Deleted: ", &channel.name);
    Ok(())
}