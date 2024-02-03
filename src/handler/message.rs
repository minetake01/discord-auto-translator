use deepl::Lang;
use poise::serenity_prelude::{Context, Message, MessageType};
use sea_orm::{ActiveModelTrait, ColumnTrait, EntityTrait, ModelTrait, QueryFilter, Set};

use crate::{
    entities::{
        guild::Entity as GuildEntity,
        translation_group::Entity as GroupEntity,
        channel::{
            Entity as ChannelEntity,
            Column as ChannelColumn,
        },
        message::ActiveModel as MessageActiveModel,
    }, error::DatabaseModelError, features::get_webhook::get_webhook, intermediate::message::IntermediateMessage, AppError, Data
};

pub async fn message(ctx: &Context, data: &Data, new_message: &Message) -> Result<(), AppError> {
    // システムメッセージは無視
    if new_message.kind != MessageType::Regular { return Ok(()) }

    // メッセージのIDがチャンネルのIDと同じ場合（フォーラムの最初のメッセージ）は終了
    if u64::from(new_message.id) == u64::from(new_message.channel_id) { return Ok(())}

    // DBからチャンネルの情報を取得
    let Some(channel) = ChannelEntity::find_by_id(i64::from(new_message.channel_id))
        .one(&data.db)
        .await? else {
            return Ok(())
        };

    // メッセージがWebhookによるものであれば終了
    if let Some(webhook_id) = new_message.webhook_id {
        if channel.webhook_id == i64::from(webhook_id) {
            return Ok(())
        }
    }

    let source_lang = Lang::try_from(&channel.lang)?;
    
    // DBからチャンネルのグループ情報を取得
    let group = channel.find_related(GroupEntity)
        .one(&data.db)
        .await?
        .ok_or(AppError::DatabaseModel(DatabaseModelError::ChannelNotBelongToGroup(channel.channel_id)))?;

    // サーバーのDeepLキーを取得（サーバーが見つからない場合はエラー）
    let deepl_key = group.find_related(GuildEntity)
        .one(&data.db)
        .await?
        .ok_or(AppError::DatabaseModel(DatabaseModelError::GroupNotBelongToGuild(group.group_name.clone())))?
        .deepl_key;

    // DBにメッセージを登録
    let new_message_model = MessageActiveModel {
        message_id: Set(new_message.id.into()),
        original_message_id: Set(None),
        channel_id: Set(channel.channel_id),
    };
    new_message_model.insert(&data.db).await?;

    // DBからグループに属する他のチャンネルを取得
    let siblings_channels = group.find_related(ChannelEntity)
        .filter(ChannelColumn::ChannelId.ne(channel.channel_id))
        .all(&data.db)
        .await?;

    for siblings_channel in siblings_channels {
        let target_lang = Lang::try_from(&siblings_channel.lang)?;
        
        let webhook = get_webhook(ctx, &data.db, &siblings_channel).await?;

        let translated_message = IntermediateMessage::from(new_message.clone())
            .translate(&source_lang, &target_lang, &deepl_key)
            .await?;
        let mut webhook_builder = translated_message
            .to_execute_webhook(ctx)
            .await?
            .avatar_url(new_message.author.avatar_url().unwrap_or(new_message.author.default_avatar_url()))
            .username(new_message.author.name.clone());
        if siblings_channel.parent_channel_id.is_some() {
            webhook_builder = webhook_builder.in_thread(siblings_channel.channel_id as u64);
        }
        let message = webhook.execute(ctx, true, webhook_builder).await?.ok_or(AppError::Unexpected)?;

        let new_message_model = MessageActiveModel {
            message_id: Set(message.id.into()),
            original_message_id: Set(Some(new_message.id.into())),
            channel_id: Set(siblings_channel.channel_id),
        };
        new_message_model.insert(&data.db).await?;
    }
    
    Ok(())
}