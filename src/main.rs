mod handler;
mod commands;
mod entities;

use std::env;

use deepl::DeepLApi;
use handler::event_handler;
use poise::{serenity_prelude::{GatewayIntents, Ready, Context}, FrameworkOptions, FrameworkError, Framework};
use sea_orm::{Database, DatabaseConnection};

#[macro_use]
extern crate rust_i18n;
i18n!("locales");

#[derive(thiserror::Error, Debug)]
pub enum AppError {
    #[error("{0}")]
    EnvVar(#[from] std::env::VarError),
    #[error("{0}")]
    Serenity(#[from] poise::serenity_prelude::Error),
    #[error("{0}")]
    DeepL(#[from] deepl::Error),
    #[error("{0}")]
    SeaOrm(#[from] sea_orm::error::DbErr),
}

pub struct Data {
    pub deepl: DeepLApi,
    pub db: DatabaseConnection,
}

async fn setup(ctx: &Context, _ready: &Ready, framework: &Framework<Data, AppError>) -> Result<Data, AppError> {
    //Slash Commandを登録
    poise::builtins::register_globally(&ctx.http, &framework.options().commands).await.unwrap();

    let deepl = DeepLApi::with(&env::var("DEEPL_API_KEY")?).new();

    let db = Database::connect("sqlite:./db/database.sqlite").await?;
    Ok(Data{
        deepl,
        db,
    })
}

async fn on_error<U>(err: FrameworkError<'_, U, AppError>) {
    poise::builtins::on_error(err).await.unwrap()
}

#[tokio::main]
async fn main() {
    dotenv::dotenv().ok();

    let token = if cfg!(debug_assertions) { env::var("DEBUG_DISCORD_TOKEN").unwrap() } else { env::var("DISCORD_TOKEN").unwrap() };

    let intents = GatewayIntents::non_privileged()
        | GatewayIntents::MESSAGE_CONTENT;

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
