use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "translation_group")]
pub struct Model {
    #[sea_orm(primary_key)]
    pub name: String,
    pub auto_threading: bool,
    pub translate_title: bool,
    pub reaction_proxy: bool,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_many = "super::channel::Entity")]
    Channel,
    #[sea_orm(has_many = "super::message::Entity")]
    Message,
}

impl Related<super::channel::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Channel.def()
    }
}

impl Related<super::message::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Message.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}