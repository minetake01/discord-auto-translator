use poise::FrameworkError;

#[derive(thiserror::Error, Debug)]
pub enum CommandError {
    #[error("This command can only be used in a guild.")]
    GuildOnly,
}

#[derive(thiserror::Error, Debug)]
pub enum DatabaseModelError {
    #[error("Message ID: {0} does not belong to the channel.")]
    MessageNotBelongToChannel(u64),
    #[error("Channel ID: {0} does not belong to the group.")]
    ChannelNotBelongToGroup(u64),
    #[error("Group Name: {0} does not belong to the guild.")]
    GroupNotBelongToGuild(String),
}

#[derive(thiserror::Error, Debug)]
pub enum AppError {
    #[error("{0}")]
    EnvVar(#[from] std::env::VarError),
    #[error("{0}")]
    Serenity(#[from] poise::serenity_prelude::Error),
    #[error("{0}")]
    DeepL(#[from] deepl::Error),
    #[error("{0}")]
    DeepLConvert(#[from] deepl::LangConvertError),
    #[error("{0}")]
    SeaOrm(#[from] sea_orm::error::DbErr),
    #[error("{0}")]
    DatabaseModel(#[from] DatabaseModelError),
    #[error("{0}")]
    Command(#[from] CommandError),

    #[error("Unexpected error")]
    Unexpected,
}

pub async fn on_error<U>(err: FrameworkError<'_, U, AppError>) {
    match err {
        FrameworkError::EventHandler { error, .. } => {
            println!("Error in event handler: {:?}", error);
        },
        FrameworkError::Command { error, .. } => {
            println!("Command Error: {:?}", error);
        },
        _ => {
            println!("{}", err);
        },
    }
}