use poise::serenity_prelude::GuildChannel;
use sea_orm::EntityTrait;

use crate::{AppError, Data, entities::channel::Entity as ChannelEntity};

pub async fn channel_delete(data: &Data, channel: &GuildChannel) -> Result<(), AppError> {
    ChannelEntity::delete_by_id(channel.id)
        .exec(&data.db)
        .await?;

    dbg!("Channel Deleted: ", &channel.name);
    Ok(())
}