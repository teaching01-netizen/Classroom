pub mod room_manager;

use room_manager::{RoomManager, RoomManagerEvent};
use qr_command_center_core::Room;
use qr_command_center_db::PgRoomRepository;
use qr_command_center_warwick::{WarwickAuth, WarwickQrClient};
use axum::{
    Router,
    routing::{get, post},
    extract::{State, WebSocketUpgrade, ws::{WebSocket, Message}, Path},
    response::IntoResponse,
    Json,
};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tower_http::cors::{CorsLayer, Any};
use tower_http::services::ServeDir;
use uuid::Uuid;
use tracing::{info, error};

async fn setup_database(pool: &sqlx::PgPool) -> Result<(), sqlx::Error> {
    let table_exists = sqlx::query_scalar::<_, bool>(
        "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'rooms')"
    )
    .persistent(false)
    .fetch_one(pool)
    .await?;

    if !table_exists {
        sqlx::query(
            "CREATE TYPE room_status AS ENUM ('idle', 'running', 'fetching', 'warning', 'auth_expired', 'stopped')"
        )
        .persistent(false)
        .execute(pool)
        .await?;

        sqlx::query(
            r#"
            CREATE TABLE rooms (
                room_id UUID PRIMARY KEY,
                class_id TEXT NOT NULL,
                name TEXT,
                status room_status NOT NULL DEFAULT 'idle',
                qr_url TEXT,
                expires_at TIMESTAMPTZ,
                last_updated_at TIMESTAMPTZ,
                warning_message TEXT,
                error_message TEXT,
                last_fetch_at TIMESTAMPTZ,
                created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
            )
            "#
        )
        .persistent(false)
        .execute(pool)
        .await?;
    }

    Ok(())
}

#[derive(Clone)]
struct AppState {
    room_manager: Arc<RoomManager<PgRoomRepository, WarwickQrClient>>,
}

#[derive(Deserialize)]
struct CreateRoomRequest {
    class_id: String,
    name: Option<String>,
}

#[derive(Serialize)]
struct ApiResponse<T> {
    success: bool,
    data: Option<T>,
    error: Option<String>,
}

async fn create_room(
    State(state): State<AppState>,
    Json(req): Json<CreateRoomRequest>,
) -> impl IntoResponse {
    info!("Creating room with class_id: {}, name: {:?}", req.class_id, req.name);
    match state.room_manager.create_room(req.class_id, req.name).await {
        Ok(room) => {
            info!("Room created successfully: {:?}", room);
            Json(ApiResponse {
                success: true,
                data: Some(room),
                error: None,
            })
        },
        Err(e) => {
            error!("Failed to create room: {:?}", e);
            Json(ApiResponse::<Room> {
                success: false,
                data: None,
                error: Some(e.to_string()),
            })
        },
    }
}

async fn delete_room(
    State(state): State<AppState>,
    Path(room_id): Path<Uuid>,
) -> impl IntoResponse {
    match state.room_manager.delete_room(room_id).await {
        Ok(_) => Json(ApiResponse {
            success: true,
            data: None::<()>,
            error: None,
        }),
        Err(e) => Json(ApiResponse {
            success: false,
            data: None::<()>,
            error: Some(e.to_string()),
        }),
    }
}

async fn get_room(
    State(state): State<AppState>,
    Path(room_id): Path<Uuid>,
) -> impl IntoResponse {
    match state.room_manager.get_room(room_id).await {
        Some(room) => Json(ApiResponse {
            success: true,
            data: Some(room),
            error: None,
        }),
        None => Json(ApiResponse::<Room> {
            success: false,
            data: None,
            error: Some("Room not found".to_string()),
        }),
    }
}

async fn start_room(
    State(state): State<AppState>,
    Path(room_id): Path<Uuid>,
) -> impl IntoResponse {
    match state.room_manager.start_room(room_id).await {
        Ok(_) => Json(ApiResponse {
            success: true,
            data: None::<()>,
            error: None,
        }),
        Err(e) => Json(ApiResponse {
            success: false,
            data: None::<()>,
            error: Some(e),
        }),
    }
}

async fn stop_room(
    State(state): State<AppState>,
    Path(room_id): Path<Uuid>,
) -> impl IntoResponse {
    match state.room_manager.stop_room(room_id).await {
        Ok(_) => Json(ApiResponse {
            success: true,
            data: None::<()>,
            error: None,
        }),
        Err(e) => Json(ApiResponse {
            success: false,
            data: None::<()>,
            error: Some(e),
        }),
    }
}

async fn get_rooms(
    State(state): State<AppState>,
) -> impl IntoResponse {
    let rooms = state.room_manager.get_all_rooms().await;
    Json(ApiResponse {
        success: true,
        data: Some(rooms),
        error: None,
    })
}

async fn ws_handler(
    State(state): State<AppState>,
    ws: WebSocketUpgrade,
) -> impl IntoResponse {
    let room_manager = state.room_manager.clone();
    ws.on_upgrade(move |socket| handle_socket(socket, room_manager))
}

async fn handle_socket(mut socket: WebSocket, room_manager: Arc<RoomManager<PgRoomRepository, WarwickQrClient>>) {
    info!("New WebSocket connection established");
    let mut rx = room_manager.subscribe();

    let rooms = room_manager.get_all_rooms().await;
    info!("Sending FullStateSync with {} rooms", rooms.len());
    let _ = socket.send(Message::Text(serde_json::to_string(&RoomManagerEvent::FullStateSync(rooms)).unwrap())).await;

    loop {
        tokio::select! {
            result = rx.recv() => {
                match result {
                    Ok(event) => {
                        info!("Sending WebSocket event: {:?}", event);
                        let _ = socket.send(Message::Text(serde_json::to_string(&event).unwrap())).await;
                    }
                    Err(_) => break,
                }
            }
            msg = socket.recv() => {
                match msg {
                    Some(Ok(Message::Close(_))) | Some(Err(_)) => {
                        info!("WebSocket connection closed");
                        break;
                    },
                    _ => {}
                }
            }
        }
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_max_level(tracing::Level::DEBUG)
        .init();

    dotenvy::dotenv().ok();

    info!("Starting QR Command Center server...");

    // Initialize Warwick auth (validates env vars exist)
    info!("Validating Warwick credentials at startup...");
    let auth = match WarwickAuth::from_env() {
        Ok(auth) => {
            match auth.get_valid_session().await {
                Ok(_) => info!("✅ Warwick authentication successful"),
                Err(e) => {
                    error!("❌ Warwick authentication failed: {}", e);
                    error!("Check WARWICK_EMAIL and WARWICK_PASSWORD in your .env file");
                    std::process::exit(1);
                }
            }
            auth
        }
        Err(e) => {
            error!("❌ Failed to initialize Warwick auth: {}", e);
            error!("Set WARWICK_EMAIL and WARWICK_PASSWORD in your .env file");
            std::process::exit(1);
        }
    };

    let qr_client = WarwickQrClient::new(auth);

    let database_url = std::env::var("DATABASE_URL").expect("DATABASE_URL must be set");
    let options = database_url.parse::<sqlx::postgres::PgConnectOptions>().expect("Invalid DATABASE_URL");
    let pool = sqlx::PgPool::connect_with(options).await.expect("Failed to connect to database");

    info!("Connected to database");
    setup_database(&pool).await.expect("Failed to setup database");

    let repository = PgRoomRepository::new(pool);
    let room_manager = Arc::new(RoomManager::new(qr_client, repository));

    room_manager.load_rooms_from_db().await.expect("Failed to load rooms from database");

    let state = AppState { room_manager };

    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    async fn root() -> impl IntoResponse {
        Json(serde_json::json!({
            "success": true,
            "message": "QR Command Center API is running!"
        }))
    }

    let app = Router::new()
        .route("/api/", get(root))
        .route("/api/rooms", get(get_rooms).post(create_room))
        .route("/api/rooms/:id", get(get_room).delete(delete_room))
        .route("/api/rooms/:id/start", post(start_room))
        .route("/api/rooms/:id/stop", post(stop_room))
        .route("/ws", get(ws_handler))
        .fallback_service(ServeDir::new("web/dist").append_index_html_on_directories(true))
        .layer(cors)
        .with_state(state);

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    info!("🚀 Server running!");
    info!("📡 Backend API: http://0.0.0.0:3000/api");
    info!("🌐 Frontend: http://0.0.0.0:3000");
    info!("🔌 WebSocket: ws://0.0.0.0:3000/ws");
    axum::serve(listener, app).await.unwrap();
}
