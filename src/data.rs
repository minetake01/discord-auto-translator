use std::{collections::HashMap, fs::OpenOptions, io::{Read, Write}, env};

use deepl::{Lang, DeepLApi};
use poise::serenity_prelude::{ChannelId, GuildId, MessageId, Mutex};
use serde::{Deserialize, Serialize};

pub struct Data {
    pub deepl: DeepLApi,
    pub guild_map: Mutex<GuildMap>,
}

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct GuildMap (HashMap<GuildId, GuildSettings>);

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct GuildSettings {
    pub reaction_proxy: bool,
    pub channel_map: ChannelMap,
    pub group_map: GroupMap,
}

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct ChannelMap (HashMap<ChannelId, ChannelAttrs>);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChannelAttrs {
    pub group_name: String,
    pub lang: Lang,
    pub auto_threading: bool,
    pub thread_title: bool,
}

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct GroupMap (HashMap<String, GroupData>);

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct GroupData {
    pub channels: Vec<ChannelId>,
    pub messages: HashMap<MessageId, Vec<MessageId>>,
}


impl Data {
    pub fn init() -> Self {
        let mut file = OpenOptions::new()
            .read(true)
            .create(true)
            .open("db/guild_map.toml")
            .unwrap();
        let mut contents = String::new();
        file.read_to_string(&mut contents).unwrap();
        let guild_map: GuildMap = toml::from_str(&contents).unwrap_or_default();

        Self {
            deepl: DeepLApi::with(&env::var("DEEPL_TOKEN").unwrap()).new(),
            guild_map: Mutex::new(guild_map),
        }
    }
}

impl GuildMap {
    pub fn save(&self) {
        let mut file = OpenOptions::new()
            .write(true)
            .create(true)
            .open("db/guild_map.toml")
            .unwrap();
        let toml = toml::to_string(self).unwrap();
        write!(file, "{}", toml).unwrap();
    }

    pub fn guild(&self, guild_id: &GuildId) -> Option<&GuildSettings> {
        self.0.get(guild_id)
    }

    pub fn guild_mut(&mut self, guild_id: &GuildId) -> Option<&mut GuildSettings> {
        self.0.get_mut(guild_id)
    }

    pub fn channel(&self, guild_id: &GuildId, channel_id: &ChannelId) -> Option<&ChannelAttrs> {
        self.guild(guild_id)?.channel_map.channel(channel_id)
    }

    pub fn add_channel(
        &mut self,
        guild_id: GuildId,
        channel_id: ChannelId,
        channel_attrs: ChannelAttrs,
    ) {
        let guild_settings = self.0.entry(guild_id).or_insert(GuildSettings::default());
        guild_settings.channel_map.0.insert(channel_id, channel_attrs.clone());

        let group_data = guild_settings.group_map.0.entry(channel_attrs.group_name).or_insert(Default::default());
        group_data.channels.push(channel_id);
        self.save();
    }

    pub fn remove_channel(&mut self, guild_id: &GuildId, channel_id: &ChannelId) -> Option<()> {
        let guild_settings = self.0.get_mut(guild_id)?;
        if let Some(channel_attrs) = guild_settings.channel_map._remove_channel(channel_id) {
            guild_settings.group_map._remove_channel(&channel_attrs.group_name, channel_id);
        }
        if guild_settings.channel_map.0.len() == 0 {
            self.0.remove(guild_id);
        }
        self.save();
        Some(())
    }

    pub fn message(&self, guild_id: &GuildId, channel_id: &ChannelId, message_id: &MessageId) -> Option<&Vec<MessageId>> {
        let guild_settings = self.guild(guild_id)?;
        let group_name = guild_settings.channel_map.channel(channel_id)?.group_name.clone();
        guild_settings.group_map.name(&group_name)?.messages.get(message_id)
    }

    pub fn add_or_update_message(
        &mut self,
        guild_id: &GuildId,
        channel_id: &ChannelId,
        message_id: MessageId,
        translated_msgs: Vec<MessageId>
    ) -> Option<()> {
        let guild_settings = self.guild_mut(guild_id)?;
        let group_name = guild_settings.channel_map.channel(channel_id)?.group_name.clone();

        let group_data = guild_settings.group_map._name_mut(&group_name)?;
        group_data.messages.insert(message_id, translated_msgs);
        self.save();
        Some(())
    }

    pub fn remove_message(&mut self, guild_id: &GuildId, channel_id: &ChannelId, message_id: &MessageId) -> Option<Vec<MessageId>> {
        let guild_settings = self.guild_mut(guild_id)?;
        let group_name = guild_settings.channel_map.channel(channel_id)?.group_name.clone();

        let group_data = guild_settings.group_map._name_mut(&group_name)?;
        let message_ids = group_data.messages.remove(message_id)?;
        self.save();
        Some(message_ids)
    }
}

impl ChannelMap {
    pub fn channel(&self, channel_id: &ChannelId) -> Option<&ChannelAttrs> {
        self.0.get(channel_id)
    }

    fn _channel_mut(&mut self, channel_id: &ChannelId) -> Option<&mut ChannelAttrs> {
        self.0.get_mut(channel_id)
    }

    fn _add_channel(&mut self, channel_id: ChannelId, channel_attrs: ChannelAttrs) {
        self.0.insert(channel_id, channel_attrs);
    }

    fn _remove_channel(&mut self, channel_id: &ChannelId) -> Option<ChannelAttrs> {
        self.0.remove(channel_id)
    }
}

impl GroupMap {
    pub fn name(&self, group_name: &str) -> Option<&GroupData> {
        self.0.get(group_name)
    }

    fn _name_mut(&mut self, group_name: &str) -> Option<&mut GroupData> {
        self.0.get_mut(group_name)
    }

    fn _remove_channel(&mut self, group_name: &str, channel_id: &ChannelId) -> Option<()> {
        let group_data = self.0.get_mut(group_name)?;
        group_data.channels.retain(|c| c != channel_id);
        if group_data.channels.len() == 0 {
            self.0.remove(group_name);
        }
        Some(())
    }
}