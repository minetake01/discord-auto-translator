use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "translation_group")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub group_name: String,
	pub guild_id: i64,
    pub auto_threading: bool,
    pub translate_title: bool,
    pub reaction_agent: bool,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(
        belongs_to = "super::guild::Entity",
        from = "Column::GuildId",
        to = "super::guild::Column::GuildId",
    )]
    Guild,
    #[sea_orm(has_many = "super::channel::Entity", on_delete = "Cascade")]
    Channel,
}

impl Related<super::guild::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Guild.def()
    }
}

impl Related<super::channel::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Channel.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}