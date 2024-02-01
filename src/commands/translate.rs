use poise::serenity_prelude::{Channel, CreateWebhook, Role};
use sea_orm::{ActiveModelTrait, Set};
use secrecy::ExposeSecret;

use crate::{
    entities::{
        guild::ActiveModel as GuildActiveModel,
        translation_group::ActiveModel as GroupActiveModel,
        channel::ActiveModel as ChannelActiveModel,
    }, error::CommandError, AppError, Data
};

type Context<'a> = poise::Context<'a, Data, AppError>;

#[poise::command(
    slash_command,
    hide_in_help,
    subcommands("init", "new", "add")
)]
pub async fn translate(_: Context<'_>) -> Result<(), AppError> { Ok(()) }

#[poise::command(
    slash_command,
)]
async fn init(
    ctx: Context<'_>,
    deepl_key: String,
    deepl_pro: Option<bool>,
    admin_role: Option<Role>,
    ignore_role: Option<Role>,
) -> Result<(), AppError> {
    let db = &ctx.data().db;
    let guild_id = ctx.guild_id().ok_or(AppError::Command(CommandError::GuildOnly))?;

    let new_guild = GuildActiveModel {
        guild_id: Set(guild_id.into()),
        deepl_key: Set(deepl_key),
        deepl_pro: Set(deepl_pro.unwrap_or(false)),
        admin_role: Set(admin_role.map(|role| role.id.into())),
        ignore_role: Set(ignore_role.map(|role| role.id.into())),
    };
    new_guild.insert(db).await?;

    Ok(())
}

#[poise::command(
    slash_command,
)]
async fn new(
    ctx: Context<'_>,
    name: String,
    reaction_agent: Option<bool>,
    auto_threading: Option<bool>,
    translate_title: Option<bool>,
) -> Result<(), AppError> {
    let db = &ctx.data().db;
    let guild_id = ctx.guild_id().ok_or(AppError::Command(CommandError::GuildOnly))?;

    let new_group = GroupActiveModel {
        guild_id: Set(guild_id.into()),
        group_name: Set(name),
        auto_threading: Set(auto_threading.unwrap_or(true)),
        translate_title: Set(translate_title.unwrap_or(true)),
        reaction_agent: Set(reaction_agent.unwrap_or(true)),
    };
    new_group.insert(db).await?;

    Ok(())
}

#[poise::command(
    slash_command,
)]
async fn add(
    ctx: Context<'_>,
    group_name: String,
    channel: Channel,
    lang: String,
) -> Result<(), AppError> {
    let db = &ctx.data().db;

    let webhook = channel.id().create_webhook(ctx, CreateWebhook::new("Auto Translator")).await?;

    let new_channel = ChannelActiveModel {
        channel_id: Set(channel.id().into()),
        parent_channel_id: Set(None),
        group_name: Set(group_name),
        lang: Set(lang),
        webhook_id: Set(webhook.id.into()),
        webhook_token: Set(webhook.token.clone().map(|token| token.expose_secret().clone()).unwrap_or_default()),
    };
    new_channel.insert(db).await?;
    
    Ok(())
}