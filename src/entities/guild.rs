use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel)]
#[sea_orm(table_name = "guild")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub guild_id: i64,
    pub deepl_key: String,
    pub deepl_pro: bool,
    pub admin_role: Option<i64>,
    pub ignore_role: Option<i64>,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_many = "super::translation_group::Entity", on_delete = "Cascade")]
    TranslationGroup,
}

impl Related<super::translation_group::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::TranslationGroup.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}