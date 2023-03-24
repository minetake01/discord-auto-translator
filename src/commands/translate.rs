use poise::serenity_prelude::{Error, Role};

use crate::{Data, AppError};

type Context<'a> = poise::Context<'a, Data, AppError>;

#[poise::command(
    slash_command,
    hide_in_help,
    subcommands("set", "info")
)]
pub async fn translate(_: Context<'_>) -> Result<(), AppError> { Ok(()) }

/// Set up automatic translation for this channel.
#[poise::command(
    slash_command,
    description_localized("ja", "このチャンネルに自動翻訳を設定します。"),
)]
pub async fn set(
    ctx: Context<'_>,
    #[description = "Name of the role group to create"]
    #[description_localized("ja", "作成するロールグループの名前")]
    name: String,
    #[description = "Whether to create in flexible mode"]
    #[description_localized("ja", "フレキシブルモードで作成するかどうか")]
    flexible: Option<bool>,
) -> Result<(), AppError> {
    Ok(())
}

/// Create a role group and include existing roles.
#[poise::command(
    slash_command,
    description_localized("ja", "ロールグループを作成し、既存のロールを含める。"),
)]
pub async fn info(
    ctx: Context<'_>,
    #[description = "Name of the role group to create"]
    #[description_localized("ja", "作成するロールグループの名前")]
    name: String,
    #[description = "Whether to create in flexible mode"]
    #[description_localized("ja", "フレキシブルモードで作成するかどうか")]
    flexible: Option<bool>,
) -> Result<(), AppError> {
    Ok(())
}