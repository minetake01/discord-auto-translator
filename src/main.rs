use std::env;

use deepl::{DeepLApi, Lang, TagHandling::Xml};
use regex::Regex;
use serenity::{prelude::{GatewayIntents, EventHandler, Context}, Client, async_trait, model::{prelude::{Ready, Message, ChannelId, WebhookId}}};

struct Handler {
    deepl: DeepLApi,
}

#[async_trait]
impl EventHandler for Handler {
    async fn ready(&self, ctx: Context, ready: Ready) {
        println!("{} is connected! Shard: {}", ready.user.name, ctx.shard_id);
    }

    async fn message(&self, ctx: Context, msg: Message) {
        // BOT、Webhookを除外
        if msg.webhook_id.is_some() || msg.author.bot { return };

        let mut msg_content = msg.content.clone();

        // URLをタグに置換
        let url_re = Regex::new(r"((https?://)[^\s]+)").unwrap();
        let urls: Vec<_> = url_re.find_iter(&msg.content).map(|mat| mat.as_str()).collect();
        for (index, url) in urls.iter().enumerate() {
            let tag = format!("<u>{}</u>", index);
            msg_content = msg_content.replacen(url, &tag, 1);
        }
        
        let Some(topic) = msg.channel(&ctx).await.ok().and_then(|c| c.guild().and_then(|c| c.topic)) else { return };
        let Some((source_lang, to)) = channel_topic_to_params(topic) else { return };

        for channel_id in to.iter() {
            let source_lang = Lang::try_from(&source_lang).unwrap();
            let Some(target_lang) = channel_id_to_lang(&ctx, channel_id).await else { continue };

            // DeepL API
            let response = &self.deepl
                .translate_text(&msg_content, target_lang)
                .source_lang(source_lang)
                .tag_handling(Xml)
                .await
                .unwrap();

            // タグをURLに置換
            let mut translated_msg = response.translations[0].text.clone();
            for (index, url) in urls.iter().enumerate() {
                let tag = format!("<u>{}</u>", index);
                translated_msg = translated_msg.replacen(&tag, url, 1);
            }

            let Ok(webhook_id) = get_webhook_id(&ctx, channel_id).await else { continue };
            let Ok(webhook) = webhook_id.to_webhook(&ctx).await else { continue };
            webhook
                .execute(&ctx, false, |w| {
                    w
                        .content(&translated_msg)
                        .add_files(msg.attachments.iter().map(|attachment| attachment.url.as_str()))
                        .username(format!("{} (Auto translated)", &msg.author.name))
                        .avatar_url(&msg.author.avatar_url().unwrap_or(msg.author.default_avatar_url()))
                })
                .await
                .unwrap();
        }
    }
}

fn channel_topic_to_params(topic: String) -> Option<(String, Vec<ChannelId>)> {
    let re = Regex::new(r"\[Auto Translate\]\nlang=(\S+)\nto=\[(.+)\]").unwrap();
    let captures = re.captures(&topic)?;
    
    let source_lang = captures.get(1).map(|x| x.as_str().to_string())?;
    let to = captures.get(2).map(|x| {
            x.as_str()
                .split(", ")
                .filter_map(|n| n.parse::<ChannelId>().ok())
                .collect::<Vec<_>>()
        })?;

    Some((source_lang, to))
}

async fn channel_id_to_lang(ctx: &Context, channel_id: &ChannelId) -> Option<Lang> {
    let channel = channel_id.to_channel(ctx).await.ok()?;
    let topic = channel.guild().and_then(|c| c.topic)?;
    
    let re = Regex::new(r"\[Auto Translate\]\nlang=(\S+)").unwrap();
    let captures = re.captures(&topic)?;
    captures.get(1).and_then(|x| Lang::try_from(x.as_str()).ok())
}

async fn get_webhook_id(ctx: &Context, channel_id: &ChannelId) -> Result<WebhookId, serenity::Error> {
    let webhooks = channel_id.webhooks(ctx).await?;
    if let Some(webhook_id) = webhooks.iter().find_map(|w| {
        if let Some(user) = w.user.clone() {
            if user.id == ctx.cache.current_user_id() { return Some(w.id) }
        }
        None
    }) {
        return Ok(webhook_id)
    }

    let webhook_id = channel_id.create_webhook(ctx, format!("Auto Translator {}", channel_id)).await.map(|w| w.id)?;
    Ok(webhook_id)
}

#[tokio::main]
async fn main() {
    #[cfg(debug_assertions)]
    dotenv::dotenv().unwrap();

    let token = if cfg!(debug_assertions) { env::var("DEBUG_DISCORD_TOKEN").unwrap() } else { env::var("DISCORD_TOKEN").unwrap() };

    let intents = GatewayIntents::non_privileged()
        | GatewayIntents::GUILD_MESSAGES
        | GatewayIntents::MESSAGE_CONTENT
        | GatewayIntents::GUILD_WEBHOOKS;

    let mut client = Client::builder(token, intents)
        .event_handler(Handler {
            deepl: DeepLApi::with(&env::var("DEEPL_TOKEN").unwrap()).new(),
        })
        .await
        .expect("Error creating client");

    if let Err(err) = client.start_autosharded().await {
        println!("An error occurred while running the client: {:?}", err)
    }
}
