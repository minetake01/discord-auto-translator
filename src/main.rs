mod handler;
mod commands;
mod entities;
mod features;
mod intermediate;
mod error;
mod data;

use std::env;

use data::Data;
use error::AppError;
use poise::{serenity_prelude as serenity, FrameworkOptions, Framework};

#[macro_use]
extern crate rust_i18n;
i18n!("locales");

#[tokio::main]
async fn main() {
    // 環境変数の読み込み
    dotenvy::dotenv().ok();

    // Frameworkの初期化
    let framework = Framework::builder()
        .options(FrameworkOptions {
            commands: vec![
                commands::translate::translate(),
            ],
            event_handler: |ctx, event, framework, data| Box::pin(handler::event_handler(ctx, event, framework, data)),
            on_error: |err| Box::pin(error::on_error(err)),
            ..Default::default()
        })
        .setup(|ctx, _ready, framework| Box::pin(async move {
            //Slash Commandを登録
            poise::builtins::register_globally(&ctx.http, &framework.options().commands).await?;
            // Dataを初期化
            Data::init().await
        }))
        .build();

    // Discord Botのトークンを取得
    let token = if cfg!(debug_assertions) { env::var("DEBUG_DISCORD_TOKEN").unwrap() } else { env::var("DISCORD_TOKEN").unwrap() };
    // Gateway Intentsを設定
    let intents = serenity::GatewayIntents::non_privileged()
        | serenity::GatewayIntents::MESSAGE_CONTENT;

    // Botの起動
    serenity::ClientBuilder::new(token, intents)
        .framework(framework)
        .await
        .expect("Failed to create client")
        .start()
        .await
        .expect("Failed to start client");
}
