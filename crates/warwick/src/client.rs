use crate::auth::WarwickAuth;
use qr_command_center_core::{QrClient, QrResponse, FetchError};
use async_trait::async_trait;

const QR_ENDPOINT: &str = "https://warwick.humantix.cloud/admin/ClassAttendance/GetQRCode";

/// QR code fetcher that uses WarwickAuth for automatic session management.
/// Handles auth refresh transparently when sessions expire.
pub struct WarwickQrClient {
    auth: WarwickAuth,
    client: reqwest::Client,
    qr_endpoint: String,
}

impl WarwickQrClient {
    /// Create a new client with the default QR endpoint.
    pub fn new(auth: WarwickAuth) -> Self {
        let client = reqwest::Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .expect("Failed to build QR HTTP client");
        Self {
            auth,
            client,
            qr_endpoint: QR_ENDPOINT.to_string(),
        }
    }

    /// Create a new client with a custom QR endpoint (for testing).
    pub fn new_with_endpoint(auth: WarwickAuth, qr_endpoint: String) -> Self {
        let client = reqwest::Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .expect("Failed to build QR HTTP client");
        Self {
            auth,
            client,
            qr_endpoint,
        }
    }

    /// Expose the auth for health checks (e.g., checking if session is valid at startup).
    pub fn auth(&self) -> &WarwickAuth {
        &self.auth
    }

    async fn do_fetch(&self, class_id: &str, cookie: &str) -> Result<QrResponse, FetchError> {
        let response = self.client
            .post(&self.qr_endpoint)
            .header("Cookie", format!("ASP.NET_SessionId={}", cookie))
            .header("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
            .header("X-Requested-With", "XMLHttpRequest")
            .body(format!("id={}", class_id))
            .send()
            .await
            .map_err(|e| FetchError::Network(e.to_string()))?;

        let status = response.status();

        if status == reqwest::StatusCode::FOUND
            || status == reqwest::StatusCode::MOVED_PERMANENTLY
        {
            return Err(FetchError::AuthExpired);
        }

        let content_type = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .unwrap_or("")
            .to_string();

        if content_type.contains("text/html") {
            let body = response.text().await.unwrap_or_default();
            if crate::auth::is_login_page(&body) {
                return Err(FetchError::AuthExpired);
            }
            return Err(FetchError::InvalidPayload(
                "Received unexpected HTML response".into(),
            ));
        }

        let qr: QrResponse = response
            .json()
            .await
            .map_err(|e| FetchError::InvalidPayload(format!("JSON parse failed: {}", e)))?;

        if qr.qrUrl.is_empty() || !qr.qrUrl.starts_with("data:image/") {
            return Err(FetchError::InvalidPayload(
                "qrUrl is empty or not a valid data URI".into(),
            ));
        }

        Ok(qr)
    }

}

#[async_trait]
impl QrClient for WarwickQrClient {
    async fn fetch_qr(&self, class_id: &str) -> Result<QrResponse, FetchError> {
        let session_cookie = self.auth
            .get_valid_session()
            .await
            .map_err(|_| FetchError::AuthExpired)?;
        self.do_fetch(class_id, &session_cookie).await
    }

    async fn fetch_qr_with_fresh_auth(&self, class_id: &str) -> Result<QrResponse, FetchError> {
        let session_cookie = self.auth
            .force_refresh()
            .await
            .map_err(|_| FetchError::AuthExpired)?;
        self.do_fetch(class_id, &session_cookie).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::auth::WarwickAuth;
    use qr_command_center_core::QrClient;
    use reqwest::Client;
    use wiremock::{Mock, MockServer, ResponseTemplate, matchers};

    #[tokio::test]
    async fn test_fetch_qr_success() {
        let auth_server = MockServer::start().await;

        // Set up auth mock (no path matcher, just POST)
        Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(302)
                    .insert_header("Location", "/admin/Home")
                    .insert_header(
                        "Set-Cookie",
                        "ASP.NET_SessionId=abc123; path=/; HttpOnly",
                    ),
            )
            .mount(&auth_server)
            .await;

        // Set up QR endpoint mock
        let qr_server = MockServer::start().await;
        Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_string(r#"{"qrUrl":"data:image/png;base64,abcd","qrTime":60}"#)
                    .insert_header("Content-Type", "application/json"),
            )
            .mount(&qr_server)
            .await;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .unwrap();

        let auth = WarwickAuth::new(
            "test@warwick.ac.th".into(),
            "password".into(),
            auth_server.uri(),
            client,
        );

        let qr_client = WarwickQrClient::new_with_endpoint(auth, qr_server.uri());
        let result = qr_client.fetch_qr("18248").await;

        assert!(result.is_ok(), "fetch_qr should succeed: {:?}", result.err());
        let qr = result.unwrap();
        assert_eq!(qr.qrUrl, "data:image/png;base64,abcd");
        assert_eq!(qr.qrTime, 60);
    }

    #[tokio::test]
    async fn test_fetch_qr_auth_expired() {
        let auth_server = MockServer::start().await;

        Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(302)
                    .insert_header("Location", "/admin/Home")
                    .insert_header(
                        "Set-Cookie",
                        "ASP.NET_SessionId=abc123; path=/; HttpOnly",
                    ),
            )
            .mount(&auth_server)
            .await;

        // QR endpoint returns 302 redirect (auth expired)
        let qr_server = MockServer::start().await;
        Mock::given(matchers::method("POST"))
            .respond_with(ResponseTemplate::new(302).insert_header("Location", "/login"))
            .mount(&qr_server)
            .await;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .unwrap();

        let auth = WarwickAuth::new(
            "test@warwick.ac.th".into(),
            "password".into(),
            auth_server.uri(),
            client,
        );

        let qr_client = WarwickQrClient::new_with_endpoint(auth, qr_server.uri());
        let result = qr_client.fetch_qr("18248").await;

        assert!(matches!(result, Err(FetchError::AuthExpired)));
    }
}
