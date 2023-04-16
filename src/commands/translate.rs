use poise::{serenity_prelude::{Channel, futures}, futures_util::{Stream, StreamExt}};

use crate::{Data, AppError};

type Context<'a> = poise::Context<'a, Data, AppError>;

async fn autocomplete_lang<'a>(
    _ctx: Context<'_>,
    partial: &'a str,
) -> impl Stream<Item = String> + 'a {
    futures::stream::iter(&["BG", "CS", "DA", "DE", "EL", "EN", "ES", "ET", "FI", "FR", "HU", "ID", "IT", "JA", "KO", "LT", "LV", "NB", "NL", "PL", "PT", "RO", "RU", "SK", "SL", "SV", "TR", "UK", "ZH"])
        .filter(move |name| futures::future::ready(name.starts_with(partial)))
        .map(|name| name.to_string())
}

#[poise::command(
    slash_command,
    hide_in_help,
    subcommands("new", "info")
)]
pub async fn translate(_: Context<'_>) -> Result<(), AppError> { Ok(()) }

/// Set up automatic translation for this channel.
#[poise::command(
    slash_command,
    description_localized("ja", "このチャンネルに自動翻訳を設定します。"),
)]
pub async fn new(
    ctx: Context<'_>,
    #[description = "Name of the automatic translation group"]
    #[description_localized("ja", "自動翻訳グループの名前")]
    group: String,
    #[description = "Channel language"]
    #[description_localized("ja", "チャンネルの言語")]
    #[autocomplete = "autocomplete_lang"]
    lang: String,
    #[description = "Channel to create an automatic translation group"]
    #[description_localized("ja", "自動翻訳グループを作成するチャンネル")]
    channel: Option<Channel>,
    #[description = "Whether to automatically create a thread for each language when a thread is created"]
    #[description_localized("ja", "スレッドが作成された時に、自動で各言語用スレッドを作成するか")]
    auto_threading: Option<bool>,
    #[description = "Whether to translate thread titles when automatically creating threads"]
    #[description_localized("ja", "自動でスレッドを作成する際、スレッドのタイトルを翻訳するか")]
    translate_title: Option<bool>,
    #[description = "Whether the BOT reacts on behalf of the target message when a reaction is attached remains uncertain"]
    #[description_localized("ja", "メッセージにリアクションが付いた時に、翻訳先のメッセージにBOTが代理でリアクションを付けるか")]
    reaction_proxy: Option<bool>,
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