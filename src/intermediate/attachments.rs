use deepl::{Lang, Error as DeepLError};
use poise::serenity_prelude::{Attachment, CreateAttachment, Error as SerenityError, Http};

use crate::features::translation::translate_text;

#[derive(Debug, Clone, Default)]
pub struct IntermediateAttachement {
    description: Option<String>,
    url: String,
}

impl From<Attachment> for IntermediateAttachement {
    fn from(attachment: Attachment) -> Self {
        IntermediateAttachement {
            description: attachment.description,
            url: attachment.url,
        }
    }
}

impl IntermediateAttachement {
    pub async fn to_create_attachment(self, http: impl AsRef<Http>) -> Result<CreateAttachment, SerenityError> {
        let mut create_attachment = CreateAttachment::url(http, &self.url).await?;
        create_attachment.description = self.description;
        Ok(create_attachment)
    }

    pub async fn translate(&self, source_lang: &Lang, target_lang: &Lang, token: &str) -> Result<Self, DeepLError> {
        let Some(description) = self.description.clone() else { return Ok(self.clone()) };

        Ok(IntermediateAttachement {
            description: Some(translate_text(description.clone(), source_lang, target_lang, token).await?),
            ..self.clone()
        })
    }
}