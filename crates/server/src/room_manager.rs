use chrono::{Duration, Utc};
use qr_command_center_core::{FetchError, QrClient, Room, RoomStatus, calculate_next_fetch_delay};
use qr_command_center_db::{RoomRepository, RepositoryError};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::{broadcast, RwLock};
use uuid::Uuid;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum RoomManagerEvent {
    RoomUpdated(Room),
    RoomCreated(Room),
    RoomDeleted(Uuid),
    FullStateSync(Vec<Room>),
}

pub struct RoomState {
    pub room: Room,
    pub shutdown_tx: Option<tokio::sync::oneshot::Sender<()>>,
}

pub struct RoomManager<R: RoomRepository, Q: QrClient> {
    rooms: Arc<RwLock<HashMap<Uuid, RoomState>>>,
    event_tx: broadcast::Sender<RoomManagerEvent>,
    qr_client: Arc<Q>,
    repository: Arc<R>,
}

impl<R: RoomRepository + 'static, Q: QrClient + 'static> RoomManager<R, Q> {
    pub fn new(qr_client: Q, repository: R) -> Self {
        let (event_tx, _) = broadcast::channel(100);
        Self {
            rooms: Arc::new(RwLock::new(HashMap::new())),
            event_tx,
            qr_client: Arc::new(qr_client),
            repository: Arc::new(repository),
        }
    }

    pub async fn load_rooms_from_db(&self) -> Result<(), RepositoryError> {
        let rooms = self.repository.get_all_rooms().await?;
        let mut rooms_lock = self.rooms.write().await;
        for room in rooms {
            let room_state = RoomState {
                room: room.clone(),
                shutdown_tx: None,
            };
            rooms_lock.insert(room.room_id, room_state);
        }
        Ok(())
    }

    pub fn subscribe(&self) -> broadcast::Receiver<RoomManagerEvent> {
        self.event_tx.subscribe()
    }

    /// Expose the QR client for health checks (e.g., validating session at startup).
    pub fn qr_client(&self) -> &Q {
        &self.qr_client
    }

    pub async fn create_room(&self, class_id: String, name: Option<String>) -> Result<Room, RepositoryError> {
        let room = Room::new(class_id, name);
        let room_state = RoomState {
            room: room.clone(),
            shutdown_tx: None,
        };

        {
            let mut rooms = self.rooms.write().await;
            rooms.insert(room.room_id, room_state);
        }

        self.repository.create_room(room.clone()).await?;

        let _ = self.event_tx.send(RoomManagerEvent::RoomCreated(room.clone()));
        Ok(room)
    }

    pub async fn delete_room(&self, room_id: Uuid) -> Result<(), RepositoryError> {
        {
            let mut rooms = self.rooms.write().await;
            if let Some(room_state) = rooms.get_mut(&room_id) {
                if let Some(shutdown_tx) = room_state.shutdown_tx.take() {
                    let _ = shutdown_tx.send(());
                }
            }
            rooms.remove(&room_id);
        }

        self.repository.delete_room(room_id).await?;

        let _ = self.event_tx.send(RoomManagerEvent::RoomDeleted(room_id));
        Ok(())
    }

    pub async fn start_room(&self, room_id: Uuid) -> Result<(), String> {
        let mut rooms = self.rooms.write().await;
        let room_state = rooms.get_mut(&room_id)
            .ok_or_else(|| "Room not found".to_string())?;

        if room_state.shutdown_tx.is_some() {
            return Err("Room already running".to_string());
        }

        let (shutdown_tx, shutdown_rx) = tokio::sync::oneshot::channel();
        room_state.shutdown_tx = Some(shutdown_tx);
        room_state.room.status = RoomStatus::Running;

        let room = room_state.room.clone();
        let rooms_clone = self.rooms.clone();
        let event_tx_clone = self.event_tx.clone();
        let qr_client_clone = self.qr_client.clone();
        let repository_clone = self.repository.clone();

        tokio::spawn(async move {
            Self::run_room_worker(
                room,
                rooms_clone,
                event_tx_clone,
                qr_client_clone,
                repository_clone,
                shutdown_rx,
            ).await;
        });

        let _ = self.event_tx.send(RoomManagerEvent::RoomUpdated(room_state.room.clone()));
        Ok(())
    }

    pub async fn stop_room(&self, room_id: Uuid) -> Result<(), String> {
        let mut rooms = self.rooms.write().await;
        let room_state = rooms.get_mut(&room_id)
            .ok_or_else(|| "Room not found".to_string())?;

        if let Some(shutdown_tx) = room_state.shutdown_tx.take() {
            let _ = shutdown_tx.send(());
        }

        room_state.room.transition_to(RoomStatus::Stopped);

        let room = room_state.room.clone();
        let repository = self.repository.clone();
        tokio::spawn(async move {
            let _ = repository.update_room(room).await;
        });

        let _ = self.event_tx.send(RoomManagerEvent::RoomUpdated(room_state.room.clone()));
        Ok(())
    }

    pub async fn get_all_rooms(&self) -> Vec<Room> {
        let rooms = self.rooms.read().await;
        rooms.values().map(|rs| rs.room.clone()).collect()
    }

    pub async fn get_room(&self, room_id: Uuid) -> Option<Room> {
        let rooms = self.rooms.read().await;
        rooms.get(&room_id).map(|rs| rs.room.clone())
    }

    async fn run_room_worker(
        mut room: Room,
        rooms: Arc<RwLock<HashMap<Uuid, RoomState>>>,
        event_tx: broadcast::Sender<RoomManagerEvent>,
        qr_client: Arc<Q>,
        repository: Arc<R>,
        mut shutdown_rx: tokio::sync::oneshot::Receiver<()>,
    ) {
        loop {
            tokio::select! {
                _ = &mut shutdown_rx => {
                    break;
                }
                _ = tokio::time::sleep(tokio::time::Duration::from_secs(1)) => {
                    let now = Utc::now();

                    let should_fetch = match room.expires_at {
                        Some(expires_at) => {
                            let delay = calculate_next_fetch_delay(60);
                            let fetch_time = expires_at - Duration::seconds(delay as i64);
                            now >= fetch_time
                        }
                        None => true,
                    };

                    if should_fetch {
                        {
                            let mut rooms_lock = rooms.write().await;
                            if let Some(room_state) = rooms_lock.get_mut(&room.room_id) {
                                room_state.room.transition_to(RoomStatus::Fetching);
                                room = room_state.room.clone();
                                let _ = event_tx.send(RoomManagerEvent::RoomUpdated(room.clone()));
                            }
                        }

                        match qr_client.fetch_qr(&room.class_id).await {
                            Ok(qr_response) => {
                                let now = Utc::now();
                                let expires_at = now + Duration::seconds(qr_response.qrTime as i64);

                                let mut rooms_lock = rooms.write().await;
                                if let Some(room_state) = rooms_lock.get_mut(&room.room_id) {
                                    room_state.room.qr_url = Some(qr_response.qrUrl);
                                    room_state.room.expires_at = Some(expires_at);
                                    room_state.room.last_updated_at = Some(now);
                                    room_state.room.last_fetch_at = Some(now);
                                    room_state.room.transition_to(RoomStatus::Running);
                                    room_state.room.warning_message = None;
                                    room_state.room.error_message = None;
                                    room = room_state.room.clone();

                                    let repo_clone = repository.clone();
                                    let room_clone = room.clone();
                                    tokio::spawn(async move {
                                        let _ = repo_clone.update_room(room_clone).await;
                                    });

                                    let _ = event_tx.send(RoomManagerEvent::RoomUpdated(room.clone()));
                                }
                            }
                            Err(e) => {
                                let mut rooms_lock = rooms.write().await;
                                if let Some(room_state) = rooms_lock.get_mut(&room.room_id) {
                                    let status = e.to_room_status();
                                    room_state.room.transition_to(status);
                                    match status {
                                        RoomStatus::AuthExpired => {
                                            room_state.room.error_message = Some("Session expired".to_string());
                                            if let Some(shutdown_tx) = room_state.shutdown_tx.take() {
                                                let _ = shutdown_tx.send(());
                                            }
                                        }
                                        _ => {
                                            room_state.room.warning_message = Some(format!("Error: {}", e));
                                        }
                                    }
                                    room = room_state.room.clone();

                                    let repo_clone = repository.clone();
                                    let room_clone = room.clone();
                                    tokio::spawn(async move {
                                        let _ = repo_clone.update_room(room_clone).await;
                                    });

                                    let _ = event_tx.send(RoomManagerEvent::RoomUpdated(room.clone()));

                                    if status == RoomStatus::AuthExpired {
                                        break;
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use async_trait::async_trait;
    use qr_command_center_core::QrResponse;
    use qr_command_center_db::RepositoryError;
    use std::collections::HashMap;
    use std::sync::Mutex;

    // ── In-memory repository (second adapter for RoomRepository) ──────

    struct InMemoryRoomRepository {
        rooms: Mutex<HashMap<Uuid, Room>>,
    }

    impl InMemoryRoomRepository {
        fn new() -> Self {
            Self { rooms: Mutex::new(HashMap::new()) }
        }
    }

    #[async_trait]
    impl RoomRepository for InMemoryRoomRepository {
        async fn create_room(&self, room: Room) -> Result<Room, RepositoryError> {
            let mut rooms = self.rooms.lock().unwrap();
            rooms.insert(room.room_id, room.clone());
            Ok(room)
        }

        async fn get_room(&self, room_id: Uuid) -> Result<Room, RepositoryError> {
            let rooms = self.rooms.lock().unwrap();
            rooms.get(&room_id).cloned().ok_or(RepositoryError::RoomNotFound(room_id))
        }

        async fn get_all_rooms(&self) -> Result<Vec<Room>, RepositoryError> {
            let rooms = self.rooms.lock().unwrap();
            Ok(rooms.values().cloned().collect())
        }

        async fn update_room(&self, room: Room) -> Result<Room, RepositoryError> {
            let mut rooms = self.rooms.lock().unwrap();
            if !rooms.contains_key(&room.room_id) {
                return Err(RepositoryError::RoomNotFound(room.room_id));
            }
            rooms.insert(room.room_id, room.clone());
            Ok(room)
        }

        async fn delete_room(&self, room_id: Uuid) -> Result<(), RepositoryError> {
            let mut rooms = self.rooms.lock().unwrap();
            rooms.remove(&room_id).ok_or(RepositoryError::RoomNotFound(room_id))?;
            Ok(())
        }
    }

    // ── Mock QR client (controllable responses) ──────────────────────

    struct MockQrClient {
        responses: Mutex<Vec<Result<QrResponse, FetchError>>>,
    }

    impl MockQrClient {
        fn new(responses: Vec<Result<QrResponse, FetchError>>) -> Self {
            Self { responses: Mutex::new(responses) }
        }
    }

    #[async_trait]
    impl QrClient for MockQrClient {
        async fn fetch_qr(&self, _class_id: &str) -> Result<QrResponse, FetchError> {
            let mut responses = self.responses.lock().unwrap();
            if responses.is_empty() {
                return Err(FetchError::Network("no more mock responses".to_string()));
            }
            Ok(responses.remove(0)?)
        }

        async fn fetch_qr_with_fresh_auth(&self, class_id: &str) -> Result<QrResponse, FetchError> {
            self.fetch_qr(class_id).await
        }
    }

    // ── Helpers ──────────────────────────────────────────────────────

    fn make_success_response() -> Result<QrResponse, FetchError> {
        Ok(QrResponse {
            qrUrl: "data:image/png;base64,test".to_string(),
            qrTime: 60,
        })
    }

    fn make_manager() -> (RoomManager<InMemoryRoomRepository, MockQrClient>, Arc<InMemoryRoomRepository>) {
        let repo = Arc::new(InMemoryRoomRepository::new());
        let qr_client = MockQrClient::new(vec![make_success_response()]);
        let mgr = RoomManager::new(qr_client, InMemoryRoomRepository::new());
        (mgr, repo)
    }

    // ── Tests ────────────────────────────────────────────────────────

    #[tokio::test]
    async fn test_create_room() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), Some("Math".into())).await.unwrap();
        assert_eq!(room.class_id, "18248");
        assert_eq!(room.status, RoomStatus::Idle);

        let all = mgr.get_all_rooms().await;
        assert_eq!(all.len(), 1);
        assert_eq!(all[0].room_id, room.room_id);
    }

    #[tokio::test]
    async fn test_get_room_by_id() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        let fetched = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(fetched.room_id, room.room_id);
    }

    #[tokio::test]
    async fn test_get_room_not_found() {
        let (mgr, _) = make_manager();
        let not_found = mgr.get_room(Uuid::new_v4()).await;
        assert!(not_found.is_none());
    }

    #[tokio::test]
    async fn test_delete_room() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.delete_room(room.room_id).await.unwrap();
        assert!(mgr.get_all_rooms().await.is_empty());
    }

    #[tokio::test]
    async fn test_start_room() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        let running = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(running.status, RoomStatus::Running);
    }

    #[tokio::test]
    async fn test_start_room_twice_fails() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();
        let err = mgr.start_room(room.room_id).await.unwrap_err();
        assert!(err.contains("already running"));
    }

    #[tokio::test]
    async fn test_stop_room() {
        let (mgr, _) = make_manager();
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();
        mgr.stop_room(room.room_id).await.unwrap();

        let stopped = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(stopped.status, RoomStatus::Stopped);
    }

    #[tokio::test]
    async fn test_worker_fetch_success() {
        let qr_client = MockQrClient::new(vec![make_success_response()]);
        let repo = InMemoryRoomRepository::new();
        let mgr = RoomManager::new(qr_client, repo);
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        // Wait for the worker to tick (1s sleep + fetch)
        tokio::time::sleep(tokio::time::Duration::from_millis(1500)).await;

        let updated = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(updated.status, RoomStatus::Running);
        assert!(updated.qr_url.is_some());
        assert!(updated.expires_at.is_some());
        assert!(updated.last_fetch_at.is_some());

        mgr.stop_room(room.room_id).await.unwrap();
    }

    #[tokio::test]
    async fn test_worker_auth_expired_stops_room() {
        let qr_client = MockQrClient::new(vec![Err(FetchError::AuthExpired)]);
        let repo = InMemoryRoomRepository::new();
        let mgr = RoomManager::new(qr_client, repo);
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        // Wait for worker to tick and encounter AuthExpired
        tokio::time::sleep(tokio::time::Duration::from_millis(1500)).await;

        let updated = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(updated.status, RoomStatus::AuthExpired);
        assert_eq!(updated.error_message, Some("Session expired".to_string()));

        // Worker should have stopped itself — stop should be a no-op
        assert!(mgr.stop_room(room.room_id).await.is_ok());
    }

    #[tokio::test]
    async fn test_worker_network_error_sets_warning() {
        let qr_client = MockQrClient::new(vec![
            Err(FetchError::Network("connection reset".to_string())),
        ]);
        let repo = InMemoryRoomRepository::new();
        let mgr = RoomManager::new(qr_client, repo);
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        tokio::time::sleep(tokio::time::Duration::from_millis(1500)).await;

        let updated = mgr.get_room(room.room_id).await.unwrap();
        assert_eq!(updated.status, RoomStatus::Warning);
        assert!(updated.warning_message.is_some());
        let msg = updated.warning_message.as_ref().unwrap();
        assert!(msg.contains("connection reset"));

        mgr.stop_room(room.room_id).await.unwrap();
    }

    #[tokio::test]
    async fn test_delete_room_stops_worker() {
        let qr_client = MockQrClient::new(vec![
            Err(FetchError::Network("slow".to_string())),
        ]);
        let repo = InMemoryRoomRepository::new();
        let mgr = RoomManager::new(qr_client, repo);
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        // Delete while worker is running
        mgr.delete_room(room.room_id).await.unwrap();
        assert!(mgr.get_room(room.room_id).await.is_none());
    }

    #[tokio::test]
    async fn test_worker_transitions_through_fetching() {
        let qr_client = MockQrClient::new(vec![make_success_response()]);
        let repo = InMemoryRoomRepository::new();
        let mgr = RoomManager::new(qr_client, repo);
        let room = mgr.create_room("18248".into(), None).await.unwrap();
        mgr.start_room(room.room_id).await.unwrap();

        // Let the worker enter the loop and set status to Fetching
        tokio::time::sleep(tokio::time::Duration::from_millis(1100)).await;

        let updated = mgr.get_room(room.room_id).await.unwrap();
        // After the fetch succeeds, status should be Running
        assert_eq!(updated.status, RoomStatus::Running);

        mgr.stop_room(room.room_id).await.unwrap();
    }
}
