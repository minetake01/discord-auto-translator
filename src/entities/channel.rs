use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "channel")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub channel_id: i64,
    pub parent_channel_id: Option<i64>,
    pub group_name: String,
    pub lang: String,
    pub webhook_id: i64,
    pub webhook_token: String,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(
        belongs_to = "Entity",
        from = "Column::ParentChannelId",
        to = "Column::ChannelId",
    )]
    ParentChannel,
    #[sea_orm(
        belongs_to = "super::translation_group::Entity",
        from = "Column::GroupName",
        to = "super::translation_group::Column::GroupName",
    )]
    TranslationGroup,
    #[sea_orm(has_many = "super::message::Entity", on_delete = "Cascade")]
    Message,
}

pub struct ParentChannelLink;

impl Linked for ParentChannelLink {
    type FromEntity = Entity;

    type ToEntity = Entity;

    fn link(&self) -> Vec<RelationDef> {
        vec![Relation::ParentChannel.def()]
    }
}

impl Related<super::translation_group::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::TranslationGroup.def()
    }
}

impl Related<super::message::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Message.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}