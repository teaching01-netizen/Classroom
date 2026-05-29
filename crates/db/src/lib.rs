use qr_command_center_core::{Room, RoomStatus};
use sqlx::PgPool;
use thiserror::Error;
use uuid::Uuid;

#[derive(Error, Debug)]
pub enum RepositoryError {
    #[error("Database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("Room not found: {0}")]
    RoomNotFound(Uuid),
}

#[async_trait::async_trait]
pub trait RoomRepository: Send + Sync {
    async fn create_room(&self, room: Room) -> Result<Room, RepositoryError>;
    async fn get_room(&self, room_id: Uuid) -> Result<Room, RepositoryError>;
    async fn get_all_rooms(&self) -> Result<Vec<Room>, RepositoryError>;
    async fn update_room(&self, room: Room) -> Result<Room, RepositoryError>;
    async fn delete_room(&self, room_id: Uuid) -> Result<(), RepositoryError>;
}

pub struct PgRoomRepository {
    pool: PgPool,
}

impl PgRoomRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait::async_trait]
impl RoomRepository for PgRoomRepository {
    async fn create_room(&self, room: Room) -> Result<Room, RepositoryError> {
        let status_str = room_status_to_str(&room.status);
        
        let room_id = room.room_id;
        let class_id = room.class_id.clone();
        let name = room.name.clone();
        
        sqlx::query(
            r#"
            INSERT INTO rooms (room_id, class_id, name, status, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at)
            VALUES ($1, $2, $3, $4::room_status, $5, $6, $7, $8, $9, $10)
            "#,
        )
        .persistent(false)
        .bind(room.room_id)
        .bind(&room.class_id)
        .bind(&room.name)
        .bind(status_str)
        .bind(&room.qr_url)
        .bind(room.expires_at)
        .bind(room.last_updated_at)
        .bind(&room.warning_message)
        .bind(&room.error_message)
        .bind(room.last_fetch_at)
        .execute(&self.pool)
        .await?;
        
        Ok(Room {
            room_id,
            class_id,
            name,
            ..room
        })
    }

    async fn get_room(&self, room_id: Uuid) -> Result<Room, RepositoryError> {
        let row = sqlx::query(
            r#"
            SELECT room_id, class_id, name, status::text, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at
            FROM rooms
            WHERE room_id = $1
            "#,
        )
        .persistent(false)
        .bind(room_id)
        .fetch_optional(&self.pool)
        .await?
        .ok_or(RepositoryError::RoomNotFound(room_id))?;
        
        Ok(row_to_room(row))
    }

    async fn get_all_rooms(&self) -> Result<Vec<Room>, RepositoryError> {
        let rows = sqlx::query(
            r#"
            SELECT room_id, class_id, name, status::text, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at
            FROM rooms
            "#,
        )
        .persistent(false)
        .fetch_all(&self.pool)
        .await?;
        
        Ok(rows.into_iter().map(row_to_room).collect())
    }

    async fn update_room(&self, room: Room) -> Result<Room, RepositoryError> {
        let status_str = room_status_to_str(&room.status);
        
        let result = sqlx::query(
            r#"
            UPDATE rooms
            SET class_id = $2, name = $3, status = $4::room_status, qr_url = $5, expires_at = $6, last_updated_at = $7, warning_message = $8, error_message = $9, last_fetch_at = $10
            WHERE room_id = $1
            "#,
        )
        .persistent(false)
        .bind(room.room_id)
        .bind(&room.class_id)
        .bind(&room.name)
        .bind(status_str)
        .bind(&room.qr_url)
        .bind(room.expires_at)
        .bind(room.last_updated_at)
        .bind(&room.warning_message)
        .bind(&room.error_message)
        .bind(room.last_fetch_at)
        .execute(&self.pool)
        .await?;
        
        if result.rows_affected() == 0 {
            return Err(RepositoryError::RoomNotFound(room.room_id));
        }
        
        Ok(room)
    }

    async fn delete_room(&self, room_id: Uuid) -> Result<(), RepositoryError> {
        let result = sqlx::query("DELETE FROM rooms WHERE room_id = $1")
            .persistent(false)
            .bind(room_id)
            .execute(&self.pool)
            .await?;
        
        if result.rows_affected() == 0 {
            return Err(RepositoryError::RoomNotFound(room_id));
        }
        
        Ok(())
    }
}

fn room_status_to_str(status: &RoomStatus) -> &'static str {
    match status {
        RoomStatus::Idle => "idle",
        RoomStatus::Running => "running",
        RoomStatus::Fetching => "fetching",
        RoomStatus::Warning => "warning",
        RoomStatus::AuthExpired => "auth_expired",
        RoomStatus::Stopped => "stopped",
    }
}

fn str_to_room_status(s: &str) -> RoomStatus {
    match s {
        "idle" => RoomStatus::Idle,
        "running" => RoomStatus::Running,
        "fetching" => RoomStatus::Fetching,
        "warning" => RoomStatus::Warning,
        "auth_expired" => RoomStatus::AuthExpired,
        "stopped" => RoomStatus::Stopped,
        _ => RoomStatus::Idle,
    }
}

fn row_to_room(row: sqlx::postgres::PgRow) -> Room {
    use sqlx::Row;
    
    Room {
        room_id: row.get("room_id"),
        class_id: row.get("class_id"),
        name: row.get("name"),
        status: str_to_room_status(row.get("status")),
        qr_url: row.get("qr_url"),
        expires_at: row.get("expires_at"),
        last_updated_at: row.get("last_updated_at"),
        warning_message: row.get("warning_message"),
        error_message: row.get("error_message"),
        last_fetch_at: row.get("last_fetch_at"),
    }
}
