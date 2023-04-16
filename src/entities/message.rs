use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "message")]
pub struct Model {
    #[sea_orm(primary_key)]
    pub id: u64,
    pub translated_id: u64,
    pub lang: String,
    pub group_name: String,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_one = "super::translation_group::Entity")]
    TranslationGroup,
}

impl Related<super::translation_group::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::TranslationGroup.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}