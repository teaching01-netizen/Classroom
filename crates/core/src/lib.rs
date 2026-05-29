use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize, de};
use thiserror::Error;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum RoomStatus {
    Idle,
    Running,
    Fetching,
    Warning,
    AuthExpired,
    Stopped,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Room {
    pub room_id: Uuid,
    pub class_id: String,
    pub name: Option<String>,
    pub status: RoomStatus,
    pub qr_url: Option<String>,
    pub expires_at: Option<DateTime<Utc>>,
    pub last_updated_at: Option<DateTime<Utc>>,
    pub warning_message: Option<String>,
    pub error_message: Option<String>,
    pub last_fetch_at: Option<DateTime<Utc>>,
}

impl Room {
    pub fn new(class_id: String, name: Option<String>) -> Self {
        Self {
            room_id: Uuid::new_v4(),
            class_id,
            name,
            status: RoomStatus::Idle,
            qr_url: None,
            expires_at: None,
            last_updated_at: None,
            warning_message: None,
            error_message: None,
            last_fetch_at: None,
        }
    }
}

pub fn calculate_next_fetch_delay(ttl: u64) -> u64 {
    (ttl * 3) / 4
}

// ── State machine ────────────────────────────────────────────────────

impl RoomStatus {
    pub fn can_transition_to(self, next: RoomStatus) -> Result<(), TransitionError> {
        use RoomStatus::*;
        let allowed = match self {
            Idle => matches!(next, Running | Stopped),
            Running => matches!(next, Fetching | Stopped),
            Fetching => matches!(next, Running | Warning | AuthExpired | Stopped),
            Warning => matches!(next, Fetching | Stopped),
            AuthExpired => matches!(next, Stopped),
            Stopped => matches!(next, Running),
        };
        if allowed {
            Ok(())
        } else {
            Err(TransitionError::IllegalTransition { from: self, to: next })
        }
    }
}

impl Room {
    pub fn transition_to(&mut self, next: RoomStatus) -> &mut Self {
        self.status = next;
        self
    }
}

impl FetchError {
    pub fn to_room_status(&self) -> RoomStatus {
        match self {
            FetchError::AuthExpired => RoomStatus::AuthExpired,
            FetchError::Network(_) | FetchError::InvalidPayload(_) => RoomStatus::Warning,
        }
    }
}

#[derive(Error, Debug)]
pub enum TransitionError {
    #[error("Cannot transition from {from:?} to {to:?}")]
    IllegalTransition { from: RoomStatus, to: RoomStatus },
}

// ── QR response & fetching ────────────────────────────────────────────

#[derive(Debug, Deserialize, Clone)]
#[allow(non_snake_case)]
pub struct QrResponse {
    pub qrUrl: String,
    #[serde(deserialize_with = "deserialize_u64")]
    pub qrTime: u64,
}

fn deserialize_u64<'de, D>(deserializer: D) -> Result<u64, D::Error>
where
    D: de::Deserializer<'de>,
{
    struct U64OrString;
    impl<'de> de::Visitor<'de> for U64OrString {
        type Value = u64;
        fn expecting(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
            f.write_str("a u64 or string containing a u64")
        }
        fn visit_u64<E: de::Error>(self, v: u64) -> Result<u64, E> { Ok(v) }
        fn visit_str<E: de::Error>(self, v: &str) -> Result<u64, E> {
            v.parse::<u64>().map_err(de::Error::custom)
        }
    }
    deserializer.deserialize_any(U64OrString)
}

#[derive(Error, Debug)]
pub enum FetchError {
    #[error("Warwick session expired or invalid")]
    AuthExpired,
    #[error("Network request failed: {0}")]
    Network(String),
    #[error("Invalid response payload: {0}")]
    InvalidPayload(String),
}

/// Trait for fetching QR codes from an attendance system.
/// The implementor manages authentication lifecycle internally.
#[async_trait::async_trait]
pub trait QrClient: Send + Sync {
    async fn fetch_qr(&self, class_id: &str) -> Result<QrResponse, FetchError>;
    async fn fetch_qr_with_fresh_auth(&self, class_id: &str) -> Result<QrResponse, FetchError>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_calculate_next_fetch_delay() {
        assert_eq!(calculate_next_fetch_delay(60), 45);
        assert_eq!(calculate_next_fetch_delay(100), 75);
        assert_eq!(calculate_next_fetch_delay(120), 90);
    }

    #[test]
    fn test_qr_response_deserialize_number() {
        let json = r#"{"qrUrl":"data:image/png;base64,abc","qrTime":60}"#;
        let qr: QrResponse = serde_json::from_str(json).unwrap();
        assert_eq!(qr.qrUrl, "data:image/png;base64,abc");
        assert_eq!(qr.qrTime, 60);
    }

    #[test]
    fn test_qr_response_deserialize_string() {
        let json = r#"{"qrUrl":"data:image/png;base64,abc","qrTime":"60"}"#;
        let qr: QrResponse = serde_json::from_str(json).unwrap();
        assert_eq!(qr.qrTime, 60);
    }

    // ── State machine tests ─────────────────────────────────────────

    #[test]
    fn test_valid_transitions() {
        use RoomStatus::*;
        let cases: &[(RoomStatus, RoomStatus)] = &[
            (Idle, Running),
            (Idle, Stopped),
            (Running, Fetching),
            (Running, Stopped),
            (Fetching, Running),
            (Fetching, Warning),
            (Fetching, AuthExpired),
            (Fetching, Stopped),
            (Warning, Fetching),
            (Warning, Stopped),
            (AuthExpired, Stopped),
            (Stopped, Running),
        ];
        for &(from, to) in cases {
            assert!(
                from.can_transition_to(to).is_ok(),
                "expected valid: {:?} -> {:?}",
                from, to
            );
        }
    }

    #[test]
    fn test_invalid_transitions() {
        use RoomStatus::*;
        let cases: &[(RoomStatus, RoomStatus)] = &[
            (Idle, Idle),
            (Idle, Fetching),
            (Idle, Warning),
            (Idle, AuthExpired),
            (Running, Running),
            (Running, Idle),
            (Running, Warning),
            (Running, AuthExpired),
            (Fetching, Fetching),
            (Fetching, Idle),
            (Warning, Warning),
            (Warning, Idle),
            (Warning, Running),
            (Warning, AuthExpired),
            (AuthExpired, AuthExpired),
            (AuthExpired, Idle),
            (AuthExpired, Running),
            (AuthExpired, Fetching),
            (AuthExpired, Warning),
            (Stopped, Stopped),
            (Stopped, Idle),
            (Stopped, Fetching),
            (Stopped, Warning),
            (Stopped, AuthExpired),
        ];
        for &(from, to) in cases {
            assert!(
                from.can_transition_to(to).is_err(),
                "expected invalid: {:?} -> {:?}",
                from, to
            );
        }
    }

    #[test]
    fn test_transition_error_message() {
        let err = TransitionError::IllegalTransition {
            from: RoomStatus::Idle,
            to: RoomStatus::Fetching,
        };
        let msg = err.to_string();
        assert!(msg.contains("Idle"));
        assert!(msg.contains("Fetching"));
    }

    #[test]
    fn test_fetch_error_to_room_status() {
        assert_eq!(FetchError::AuthExpired.to_room_status(), RoomStatus::AuthExpired);
        assert_eq!(
            FetchError::Network("timeout".into()).to_room_status(),
            RoomStatus::Warning,
        );
        assert_eq!(
            FetchError::InvalidPayload("bad json".into()).to_room_status(),
            RoomStatus::Warning,
        );
    }

    #[test]
    fn test_room_transition_to() {
        let mut room = Room::new("c1".into(), None);
        assert_eq!(room.status, RoomStatus::Idle);
        room.transition_to(RoomStatus::Running);
        assert_eq!(room.status, RoomStatus::Running);
        room.transition_to(RoomStatus::Fetching);
        assert_eq!(room.status, RoomStatus::Fetching);
        room.transition_to(RoomStatus::Warning);
        assert_eq!(room.status, RoomStatus::Warning);
    }
}
