use crate::AppError;

pub fn shard_ready(total_shards: &u32) -> Result<(), AppError> {
    println!("Shard Ready. Total Shards: {}", total_shards);
    Ok(())
}