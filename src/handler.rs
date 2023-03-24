use poise::{serenity_prelude::{Context, ChannelId, WebhookId, Webhook}, FrameworkContext, Event};
use regex::Regex;
use deepl::TagHandling::Xml;

use crate::{data::{Data, ChannelAttrs}, AppError};

pub async fn event_handler(ctx: &Context, event: &Event<'_>, _framework: FrameworkContext<'_, Data, AppError>, data: &Data) -> Result<(), AppError> {
    match event {
        Event::Ready { data_about_bot } => {
            println!("{} is connected! Shard: {}", data_about_bot.user.name, ctx.shard_id);
            Ok(())
        },
        Event::CacheReady { guilds } => {
            println!("Cache Ready. {} Guilds Loaded.", guilds.len());
            Ok(())
        },
        Event::ChannelDelete { channel } => {
            let mut guild_map = data.guild_map.lock().await;
            guild_map.remove_channel(&channel.guild_id, &channel.id);
            Ok(())
        },
        Event::ThreadCreate { thread } => {
            let mut guild_map = data.guild_map.lock().await;
            let mut created_channels: Vec<(ChannelId, ChannelAttrs)> = vec![];
            {
                let Some(guild_settings) = guild_map.guild(&thread.guild_id) else { return Ok(()) };
    
                // ループ防止
                if guild_settings.channel_map.channel(&thread.id).is_some() { return Ok(()) }
    
                let Some(parent_attrs) = guild_settings.channel_map.channel(&thread.parent_id.unwrap()) else { return Ok(()) };
                let Some(group_data) = guild_settings.group_map.name(&parent_attrs.group_name) else { return Ok(()) };
                if parent_attrs.auto_threading {
                    for to_channel in &group_data.channels {
                        let Some(to_channel_attrs) = guild_settings.channel_map.channel(&to_channel) else { return Ok(()) };
    
                        let mut thread_name = thread.name.clone();
                        //スレッド名翻訳
                        if parent_attrs.thread_title {
                            thread_name = data.deepl
                                .translate_text(thread_name, to_channel_attrs.lang.clone())
                                .source_lang(parent_attrs.lang.clone())
                                .await?
                                .translations[0]
                                .text
                                .clone();
                        }
        
                        // スレッドのIDは元メッセージのIDと同じらしい。
                        let created_thread = to_channel.create_public_thread(&ctx, thread.id.0, |t| {
                            t
                                .name(thread_name)
                                .kind(thread.kind)
                        }).await?;

                        created_channels.push((created_thread.id, to_channel_attrs.clone()));
                    }
                }
            }
            for (channel_id, channel_attrs) in created_channels {
                guild_map.add_channel(thread.guild_id, channel_id, channel_attrs);
            }
            Ok(())
        },
        Event::ThreadDelete { thread } => {
            let mut guild_map = data.guild_map.lock().await;
            guild_map.remove_channel(&thread.guild_id, &thread.id);
            Ok(())
        },
        Event::ThreadUpdate { thread } => {
            let guild_map = data.guild_map.lock().await;
            let Some(guild_settings) = guild_map.guild(&thread.guild_id) else { return Ok(()) };
            let Some(channel_attrs) = guild_settings.channel_map.channel(&thread.id) else { return Ok(()) };
            let Some(group_data) = guild_settings.group_map.name(&channel_attrs.group_name) else { return Ok(()) };
            if channel_attrs.thread_title {
                for to_channel_id in &group_data.channels {
                    let Some(to_channel_attrs) = guild_settings.channel_map.channel(&to_channel_id) else { return Ok(()) };
                    //スレッド名翻訳
                    let thread_name = data.deepl
                        .translate_text(thread.name(), to_channel_attrs.lang.clone())
                        .source_lang(channel_attrs.lang.clone())
                        .await?
                        .translations[0]
                        .text
                        .clone();

                    to_channel_id.edit_thread(&ctx, |t| {
                        t.name(thread_name)
                    }).await?;
                }
            }
            Ok(())
        },
        Event::Message { new_message } => {
            // BOT、Webhook、DMを除外
            if new_message.webhook_id.is_some() || new_message.author.bot || new_message.is_private() { return Ok(()) };

            let mut msg_content = new_message.content.clone();
            // URLをタグに置換
            let elements = convert_placeholder(&mut msg_content);
            
            let mut guild_map = data.guild_map.lock().await;
            let guild_id = new_message.guild_id.unwrap();
            let Some(guild_settings) = guild_map.guild_mut(&guild_id) else { return Ok(()) };

            let Some(channel_attrs) = guild_settings.channel_map.channel(&new_message.channel_id) else { return Ok(()) };
            let Some(group_data) = guild_settings.group_map.name(&channel_attrs.group_name) else { return Ok(()) };
            
            let mut message_ids = vec![];
            for to_channel_id in group_data.channels.iter() {
                let Some(to_channel_attrs) = guild_settings.channel_map.channel(to_channel_id) else { return Ok(()) };
                // メッセージ翻訳
                let translated_msg = &mut data.deepl
                    .translate_text(&msg_content, to_channel_attrs.lang.clone())
                    .source_lang(channel_attrs.lang.clone())
                    .tag_handling(Xml)
                    .await?
                    .translations[0]
                    .text;

                // タグをURLに置換
                revert_placeholder(translated_msg, &elements);

                let Ok(webhook) = get_webhook(&ctx, to_channel_id).await else { continue };
                let Some(message) = webhook
                    .execute(&ctx, false, |w| {
                        w
                            .content(&translated_msg)
                            .add_files(new_message.attachments.iter().map(|attachment| attachment.url.as_str()))
                            .username(format!("{} (Auto translated)", new_message.author.name))
                            .avatar_url(new_message.author.avatar_url().unwrap_or(new_message.author.default_avatar_url()))
                    }).await? else { continue };
                
                message_ids.push(message.id);
            }
            guild_map.add_or_update_message(&guild_id, &new_message.channel_id, new_message.id, message_ids);
            Ok(())
        },
        Event::MessageDelete { channel_id, deleted_message_id, guild_id } => {
            let Some(guild_id) = guild_id else { return Ok(()) };
            let mut guild_map = data.guild_map.lock().await;
            let Some(message_ids) = guild_map.remove_message(guild_id, channel_id, deleted_message_id) else { return Ok(()) };
            for message_id in message_ids {
                let Ok(webhook) = get_webhook(&ctx, channel_id).await else { continue };
                webhook.delete_message(&ctx, message_id).await?;
            }
            Ok(())
        },
        Event::MessageDeleteBulk { channel_id, multiple_deleted_messages_ids, guild_id } => {
            let Some(guild_id) = guild_id else { return Ok(()) };
            let mut guild_map = data.guild_map.lock().await;
            for message_id in multiple_deleted_messages_ids.iter() {
                let Some(message_ids) = guild_map.remove_message(guild_id, channel_id, message_id) else { continue };
                for message_id in message_ids {
                    let Ok(webhook) = get_webhook(&ctx, channel_id).await else { continue };
                    webhook.delete_message(&ctx, message_id).await?;
                }
            }
            Ok(())
        },
        Event::MessageUpdate { old_if_available: _, new, event } => {
            let Some(message) = new else { return Ok(()) };
            // BOT、Webhook、DMを除外
            if message.webhook_id.is_some() || message.author.bot || message.is_private() { return Ok(()) };
            Ok(())
        },
        Event::ReactionAdd { add_reaction } => todo!(),
        Event::ReactionRemove { removed_reaction } => todo!(),
        Event::ReactionRemoveAll { channel_id, removed_from_message_id } => todo!(),
        Event::GuildDelete { incomplete, full } => todo!(),
        event => {
            #[cfg(debug_assertions)]
            println!("{}", event.name());
            Ok(())
        },
    }
}

fn convert_placeholder(msg: &mut String) -> Vec<String> {
    let url_re = Regex::new(r"((https?://)[^\s]+)").unwrap();
    let elements: Vec<_> = url_re.find_iter(&msg).map(|mat| mat.as_str().to_string()).collect();
    for (index, element) in elements.iter().enumerate() {
        let tag = format!("<p i=\"{}\"></p>", index);
        *msg = msg.replacen(element, &tag, 1);
    }
    elements
}

fn revert_placeholder(msg: &mut String, urls: &Vec<String>) {
    for (index, url) in urls.iter().enumerate() {
        let tag = format!("<u id=\"{}\"></u>", index);
        *msg = msg.replacen(&tag, url, 1);
    }
}

async fn get_webhook(ctx: &Context, channel_id: &ChannelId) -> Result<Webhook, AppError> {
    let webhooks = channel_id.webhooks(ctx).await?;
    if let Some(webhook) = webhooks.iter().find_map(|w| {
        if let Some(user) = w.user.clone() {
            if user.id == ctx.cache.current_user_id() { return Some(w) }
        }
        None
    }) {
        return Ok(webhook.clone())
    }
    let webhook = channel_id.create_webhook(ctx, format!("Auto Translator {}", channel_id)).await?;
    Ok(webhook)
}