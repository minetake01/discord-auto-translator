use deepl::Lang;
use poise::serenity_prelude::{ChannelId, ChannelType, Context, CreateThread, Error, GuildChannel};
use sea_orm::{ActiveModelTrait, ColumnTrait, EntityTrait, ModelTrait, QueryFilter, Set};
use secrecy::ExposeSecret;

use crate::{
    entities::{
        guild::Entity as GuildEntity,
        channel::{
            ActiveModel as ChannelActiveModel,
            Column as ChannelColumn,
            Entity as ChannelEntity
        },
        translation_group::{
            Entity as GroupEntity,
            ActiveModel as GroupActiveModel,
        },
        message::{
            Entity as MessageEntity,
            Column as MessageColumn,
            ActiveModel as MessageActiveModel,
        },
    },
    error::DatabaseModelError,
    features::{
        get_webhook::get_webhook,
        translation::translate_text,
    },
    intermediate::message::IntermediateMessage,
    AppError,
    Data,
};

pub async fn thread_create(ctx: &Context, data: &Data, thread: &GuildChannel) -> Result<(), AppError> {
    // スレッドのオーナーがBOT自身か、存在しない（Webhook）場合は終了
    if let Some(owner_id) = thread.owner_id {
        match owner_id.to_user(ctx).await {
            Ok(user) => {
                if user.id == ctx.cache.current_user().id {
                    return Ok(())
                }
            },
            Err(Error::Http(_)) => return Ok(()),
            Err(err) => return Err(err.into()),
        }
    }
    
    // スレッドの親チャンネルIDを取得
    let Some(original_parent_id) = thread.parent_id else { return Ok(()) };
    // スレッドのメタデータを取得
    let Some(original_thread_metadata) = thread.thread_metadata else { return Ok(()) };

    // DBから親チャンネルの情報を取得
    let Some(original_parent_channel) = ChannelEntity::find_by_id(original_parent_id)
        .one(&data.db)
        .await? else {
            return Ok(())
        };

    // DBから親チャンネルのグループ情報を取得
    let parent_group = original_parent_channel.find_related(GroupEntity)
        .one(&data.db)
        .await?
        .ok_or(AppError::DatabaseModel(DatabaseModelError::ChannelNotBelongToGroup(original_parent_channel.channel_id)))?;

    // 自動スレッド機能が無効の場合は終了
    if !parent_group.auto_threading { return Ok(()) };
    

    // 親チャンネルの言語を取得
    let source_lang = Lang::try_from(&original_parent_channel.lang)?;

    // サーバーのDeepLキーを取得（サーバーが見つからない場合はエラー）
    let deepl_key = parent_group.find_related(GuildEntity)
    .one(&data.db)
    .await?
    .ok_or(AppError::DatabaseModel(DatabaseModelError::GroupNotBelongToGuild(parent_group.group_name.clone())))?
    .deepl_key;

    // DBに新しいグループを登録
    let new_group = GroupActiveModel {
        group_name: Set(thread.id.to_string()), // グループ名はスレッドIDとする
        guild_id: Set(thread.guild_id.into()),
        auto_threading: Set(false),
        translate_title: Set(false),
        reaction_agent: Set(parent_group.reaction_agent),   // 代理リアクション設定は親グループから継承する
    };
    new_group.insert(&data.db).await?;
    
    // DBにスレッドを登録
    let new_channel = ChannelActiveModel {
        channel_id: Set(thread.id.into()),
        parent_channel_id: Set(Some(original_parent_id.into())),
        group_name: Set(thread.id.to_string()),
        lang: Set(source_lang.to_string()),
        webhook_id: Set(original_parent_channel.webhook_id),    // Webhookは親チャンネルから継承する
        webhook_token: Set(original_parent_channel.webhook_token),
    };
    new_channel.insert(&data.db).await?;
    
    // 親グループに属する他のチャンネルを取得
    let siblings_channels = parent_group.find_related(ChannelEntity)
        .filter(ChannelColumn::ChannelId.ne(original_parent_channel.channel_id))
        .all(&data.db)
        .await?;

    let Some(original_parent_channel) = original_parent_id.to_channel(ctx).await?.guild() else { return Ok(()) };

    if original_parent_channel.kind == ChannelType::Forum {
        // スレッドの最初のメッセージを取得
        let starter_message = thread.message(ctx, u64::from(thread.id)).await?;

        for siblings_channel in siblings_channels {
            let target_lang = Lang::try_from(&siblings_channel.lang)?;

            // Webhookを取得
            let webhook = get_webhook(&ctx, &data.db, &siblings_channel).await?;

            // スレッド名を翻訳
            let translated_name = translate_text(thread.name.clone(), &source_lang, &target_lang, &deepl_key).await?;
            // メッセージを翻訳
            let translated_message = IntermediateMessage::from(starter_message.clone()).translate(&source_lang, &target_lang, &deepl_key).await?;
            // メッセージを送信
            let webhook_builder = translated_message
                .to_execute_webhook(&ctx)
                .await?
                .thread_name(translated_name)
                .avatar_url(starter_message.author.avatar_url().unwrap_or(starter_message.author.default_avatar_url()))
                .username(starter_message.author.name.clone());
            let message = webhook.execute(ctx, true, webhook_builder).await?.ok_or(AppError::Unexpected)?;

            // DBにスレッドを登録
            let new_channel = ChannelActiveModel {
                channel_id: Set(message.id.into()),
                parent_channel_id: Set(Some(siblings_channel.channel_id.into())),
                group_name: Set(thread.id.to_string()),
                lang: Set(target_lang.to_string()),
                webhook_id: Set(webhook.id.into()),
                webhook_token: Set(webhook.token.clone().map(|token| token.expose_secret().clone()).unwrap_or_default()),
            };
            new_channel.insert(&data.db).await?;

            // DBにメッセージを登録
            let new_message = MessageActiveModel {
                message_id: Set(message.id.into()),
                original_message_id: Set(Some(starter_message.id.into())),
                channel_id: Set(message.id.into()),
            };
            new_message.insert(&data.db).await?;
        }
    } else {
        let starter_message = original_parent_id.message(ctx, u64::from(thread.id)).await?;

        if starter_message.kind == MessageType::Regular {
            for siblings_channel in siblings_channels {
                let target_lang = Lang::try_from(&siblings_channel.lang)?;

                // スターターメッセージの翻訳メッセージを取得
                let siblings_starter_message = siblings_channel.find_related(MessageEntity)
                    .filter(MessageColumn::OriginalMessageId.eq(i64::from(starter_message.id)))
                    .one(&data.db)
                    .await?
                    .ok_or(AppError::DatabaseModel(DatabaseModelError::MessageNotBelongToChannel(starter_message.id.into())))?;

                // スレッド名を翻訳
                let translated_name = translate_text(thread.name.clone(), &source_lang, &target_lang, &deepl_key).await?;
                // スレッドを作成
                let thread_builder = CreateThread::new(translated_name)
                    .kind(thread.kind)
                    .auto_archive_duration(original_thread_metadata.auto_archive_duration)
                    .invitable(original_thread_metadata.invitable);
                let thread = ChannelId::from(siblings_channel.channel_id as u64)
                    .create_thread_from_message(ctx, siblings_starter_message.message_id as u64, thread_builder).await?;

                // DBにスレッドを登録
                let new_channel = ChannelActiveModel {
                    channel_id: Set(thread.id.into()),
                    parent_channel_id: Set(Some(siblings_channel.channel_id.into())),
                    group_name: Set(thread.id.to_string()),
                    lang: Set(target_lang.to_string()),
                    webhook_id: Set(siblings_channel.webhook_id),
                    webhook_token: Set(siblings_channel.webhook_token),
                };
                new_channel.insert(&data.db).await?;
            }
        } else {
            for siblings_channel in siblings_channels {
                let target_lang = Lang::try_from(&siblings_channel.lang)?;

                // スレッド名を翻訳
                let translated_name = translate_text(thread.name.clone(), &source_lang, &target_lang, &deepl_key).await?;
                // スレッドを作成
                let thread_builder = CreateThread::new(translated_name)
                    .kind(thread.kind)
                    .auto_archive_duration(original_thread_metadata.auto_archive_duration)
                    .invitable(original_thread_metadata.invitable);
                let thread = ChannelId::from(siblings_channel.channel_id as u64)
                    .create_thread(ctx, thread_builder).await?;

                // DBにスレッドを登録
                let new_channel = ChannelActiveModel {
                    channel_id: Set(thread.id.into()),
                    parent_channel_id: Set(Some(siblings_channel.channel_id.into())),
                    group_name: Set(thread.id.to_string()),
                    lang: Set(target_lang.to_string()),
                    webhook_id: Set(siblings_channel.webhook_id),
                    webhook_token: Set(siblings_channel.webhook_token),
                };
                new_channel.insert(&data.db).await?;
            }
        }
    }
    Ok(())
}
