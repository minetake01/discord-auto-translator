use poise::serenity_prelude::{Context, Ready};

use crate::AppError;

pub fn ready(ctx: &Context, data_about_bot: &Ready) -> Result<(), AppError> {
    println!("{} is connected! Shard: {}", data_about_bot.user.name, ctx.shard_id);
    Ok(())
}