use deepl::{Lang, Error as DeepLError};
use poise::serenity_prelude::{Embed, ExecuteWebhook, Http, Message, MessageFlags, Error as SerenityError};

use crate::features::translation::translate_text;

use super::attachments::IntermediateAttachement;

#[derive(Clone, Debug, Default)]
pub struct IntermediateMessage {
    content: String,
    tts: bool,
    attachments: Vec<IntermediateAttachement>,
    embeds: Vec<Embed>,
    flags: Option<MessageFlags>,
}

impl From<Message> for IntermediateMessage {
    fn from(message: Message) -> Self {
        IntermediateMessage {
            content: message.content,
            tts: message.tts,
            attachments: message.attachments.into_iter().map(|attachment| attachment.into()).collect(),
            embeds: message.embeds,
            flags: message.flags,
        }
    }
}

impl IntermediateMessage {
    pub async fn to_execute_webhook(self, http: impl AsRef<Http>) -> Result<ExecuteWebhook, SerenityError> {
        // AttachmentをCreateAttachmentに変換
        let mut attachments = Vec::new();
        for attachment in self.attachments {
            attachments.push(attachment.to_create_attachment(&http).await?);
        }
        
        let mut builder = ExecuteWebhook::new()
            .content(self.content)
            .tts(self.tts)
            .files(attachments)
            .embeds(self.embeds.into_iter().map(|embed| embed.into()).collect());

        if let Some(flags) = self.flags {
            builder = builder.flags(flags);
        }

        Ok(builder)
    }

    pub async fn translate(&self, source_lang: &Lang, target_lang: &Lang, token: &str) -> Result<Self, DeepLError> {
        // メッセージを翻訳
        let content = translate_text(self.content.clone(), source_lang, target_lang, token).await?;
        // Embedを翻訳
        let mut embeds = Vec::new();
        for embed in &self.embeds {
            let mut embed = embed.clone();
            if let Some(title) = embed.title {
                embed.title = Some(translate_text(title.clone(), source_lang, target_lang, token).await?);
            }
            if let Some(description) = embed.description {
                embed.description = Some(translate_text(description.clone(), source_lang, target_lang, token).await?);
            }
            embeds.push(embed);
        }
        // Attachmentを翻訳
        let mut attachments = Vec::new();
        for attachment in &self.attachments {
            attachments.push(attachment.translate(source_lang, target_lang, token).await);
        }

        Ok(IntermediateMessage {
            content,
            attachments: self.attachments.clone(),
            embeds,
            ..self.clone()
        })
    }
}