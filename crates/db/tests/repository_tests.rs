use qr_command_center_core::Room;
use qr_command_center_db::{PgRoomRepository, RoomRepository};
use sqlx::PgPool;
use testcontainers::Docker;
use testcontainers::Image;

#[tokio::test]
async fn test_create_and_get_room() {
    let docker = Docker::default();
    let image = Image::new("postgres").with_tag("latest");
    let container = docker.run(image);
    
    let connection_string = format!(
        "postgres://postgres:postgres@localhost:{}/postgres",
        container.get_host_port_ipv4(5432)
    );
    
    let pool = PgPool::connect(&connection_string).await.unwrap();
    
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .unwrap();
    
    let repository = PgRoomRepository::new(pool);
    
    let room = Room::new("18248".to_string(), Some("SAT Math C1".to_string()));
    let created_room = repository.create_room(room.clone()).await.unwrap();
    
    assert_eq!(created_room.room_id, room.room_id);
    assert_eq!(created_room.class_id, room.class_id);
    assert_eq!(created_room.name, room.name);
    
    let fetched_room = repository.get_room(room.room_id).await.unwrap();
    
    assert_eq!(fetched_room.room_id, room.room_id);
    assert_eq!(fetched_room.class_id, room.class_id);
}
