use poise::serenity_prelude::{ChannelId, Context, CreateWebhook, Webhook};
use sea_orm::{ActiveModelTrait, DatabaseConnection, Set};
use secrecy::ExposeSecret;

use crate::{entities::channel::{Model as ChannelModel, ActiveModel as ChannelActiveModel}, AppError};

/// スレッドの場合は親チャンネルのWebhookが取得されます。
pub async fn get_webhook(ctx: &Context, db: &DatabaseConnection, channel_model: &ChannelModel) -> Result<Webhook, AppError> {
    match Webhook::from_id_with_token(ctx, channel_model.webhook_id as u64, &channel_model.webhook_token.clone()).await {
        Ok(webhook) => Ok(webhook),
        Err(poise::serenity_prelude::Error::Http(_)) => {
            // Webhookが無効な場合は新規作成
            let webhook = ChannelId::from(channel_model.channel_id as u64)
                .create_webhook(ctx, CreateWebhook::new("Auto Translator"))
                .await?;
            
            // DBにWebhookのURLを保存
            let mut channel_model: ChannelActiveModel = channel_model.clone().into();
            channel_model.webhook_id = Set(webhook.id.into());
            channel_model.webhook_token = Set(webhook.token.clone().map(|token| token.expose_secret().clone()).unwrap_or_default());
            channel_model.update(db).await?;
            
            Ok(webhook)
        },
        Err(err) => Err(err.into()),
    }
}