use poise::serenity_prelude::GuildId;

use crate::AppError;

pub fn cache_ready(guilds: &Vec<GuildId>) -> Result<(), AppError> {
    println!("Cache Ready. {} Guilds Loaded.", guilds.len());
    Ok(())
}