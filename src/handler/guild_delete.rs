use poise::serenity_prelude::{UnavailableGuild, Guild};
use sea_orm::EntityTrait;

use crate::{AppError, Data, entities::guild::Entity as GuildEntity};

pub async fn guild_delete(data: &Data, incomplete: &UnavailableGuild, full: &Option<Guild>) -> Result<(), AppError> {
    // unavailableがtrueの場合は、障害などでサーバーから切断されたとき
    if incomplete.unavailable { return Ok(()) }
    let Some(guild) = full else { return Ok(()) };

    GuildEntity::delete_by_id(guild.id)
        .exec(&data.db)
        .await?;

    dbg!("Guild Deleted: ", &guild.name);
    Ok(())
}