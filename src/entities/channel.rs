use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "channel")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub id: u64,
    pub lang: String,
    pub group_name: String,
	pub parent_id: Option<u64>,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_one = "super::guild::Entity")]
    Guild,
    #[sea_orm(has_one = "super::translation_group::Entity")]
    TranslationGroup,
}

impl Related<super::guild::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::Guild.def()
    }
}

impl Related<super::translation_group::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::TranslationGroup.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}