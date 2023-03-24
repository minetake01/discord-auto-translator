mod data;
mod handler;
mod commands;

use std::env;

use data::Data;
use handler::event_handler;
use poise::{serenity_prelude::{GatewayIntents, Ready, Context}, FrameworkOptions, FrameworkError, Framework};

#[derive(thiserror::Error, Debug)]
pub enum AppError {
    #[error("{0}")]
    Serenity(#[from] poise::serenity_prelude::Error),
    #[error("{0}")]
    DeepL(#[from] deepl::Error),
}

async fn setup(ctx: &Context, _ready: &Ready, _framework: &Framework<Data, AppError>) -> Result<Data, AppError> {
    //Slash Commandを登録
    poise::builtins::register_globally(&ctx, &[
        commands::translate::translate(),
    ]).await?;

    Ok(Data::init())
}

async fn on_error<U>(err: FrameworkError<'_, U, AppError>) {
    poise::builtins::on_error(err).await.unwrap()
}

#[tokio::main]
async fn main() {
    dotenv::dotenv().ok();

    let token = if cfg!(debug_assertions) { env::var("DEBUG_DISCORD_TOKEN").unwrap() } else { env::var("DISCORD_TOKEN").unwrap() };

    let intents = GatewayIntents::GUILD_MESSAGES
        | GatewayIntents::MESSAGE_CONTENT
        | GatewayIntents::GUILD_WEBHOOKS;

    let options = FrameworkOptions {
        commands: vec![
            commands::translate::translate(),
        ],
        event_handler: |ctx, event, framework, data| Box::pin(event_handler(ctx, event, framework, data)),
        on_error: |err| Box::pin(on_error(err)),
        ..Default::default()
    };

    Framework::builder()
        .token(token)
        .intents(intents)
        .options(options)
        .setup(|ctx, ready, framework| Box::pin(setup(ctx, ready, framework)))
        .run_autosharded()
        .await
        .unwrap()
}
