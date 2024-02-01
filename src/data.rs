use sea_orm::{ConnectionTrait, Database, DatabaseConnection, DbBackend, Schema, Statement};

use crate::{
    entities::{
        guild::Entity as GuildEntity,
        translation_group::Entity as GroupEntity,
        channel::Entity as ChannelEntity,
        message::Entity as MessageEntity,
    },
    error::AppError
};

pub struct Data {
    pub db: DatabaseConnection,
}

impl Data {
    pub async fn init() -> Result<Data, AppError> {
        // Databaseに接続
        let db = Database::connect("sqlite:./db/database.sqlite").await?;
        // Foreign Keyを有効化
        db.execute(Statement::from_string(
            DbBackend::Sqlite,
            String::from("PRAGMA foreign_keys = ON;")
        )).await?;
    
        let builder = db.get_database_backend();
        let schema = Schema::new(builder);
    
        // テーブルを作成
        let guild_stmt = schema.create_table_from_entity(GuildEntity);
        db.execute(builder.build(&guild_stmt)).await.ok();
        let group_stmt = schema.create_table_from_entity(GroupEntity);
        db.execute(builder.build(&group_stmt)).await.ok();
        let channel_stmt = schema.create_table_from_entity(ChannelEntity);
        db.execute(builder.build(&channel_stmt)).await.ok();
        let message_stmt = schema.create_table_from_entity(MessageEntity);
        db.execute(builder.build(&message_stmt)).await.ok();
    
        Ok(Data {
            db,
        })
    }
}