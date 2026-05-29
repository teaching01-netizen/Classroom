// Warwick authentication scraper.
// Reads WARWICK_EMAIL and WARWICK_PASSWORD from environment,
// POSTs to /admin/ with form data, captures the ASP.NET_SessionId cookie,
// and provides auto-refreshing session management.

use std::sync::Arc;
use tokio::sync::RwLock;
use reqwest::Client;
use anyhow::{anyhow, Context, Result};
use chrono::{DateTime, Duration, Utc};
use tracing::{info, warn};

const LOGIN_URL: &str = "https://warwick.humantix.cloud/admin/";
const SESSION_COOKIE_NAME: &str = "ASP.NET_SessionId";
/// Refresh session 5 minutes before typical 20-min ASP.NET timeout
const SESSION_REFRESH_BUFFER: Duration = Duration::minutes(5);

#[derive(Debug, Clone)]
struct SessionState {
    cookie_value: String,
    #[allow(dead_code)]
    obtained_at: DateTime<Utc>,
    expires_at: DateTime<Utc>,
}

/// Manages Warwick authentication by scraping the login form
/// and maintaining a valid session cookie.
#[derive(Clone)]
pub struct WarwickAuth {
    client: Client,
    email: String,
    password: String,
    login_url: String,
    session: Arc<RwLock<Option<SessionState>>>,
}

impl WarwickAuth {
    /// Initialize from environment variables. Fails fast if missing.
    pub fn from_env() -> Result<Self> {
        let email = std::env::var("WARWICK_EMAIL")
            .context("WARWICK_EMAIL not set in environment")?;
        let password = std::env::var("WARWICK_PASSWORD")
            .context("WARWICK_PASSWORD not set in environment")?;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .context("Failed to build HTTP client")?;

        Ok(Self {
            client,
            email,
            password,
            login_url: LOGIN_URL.to_string(),
            session: Arc::new(RwLock::new(None)),
        })
    }

    /// Initialize with explicit credentials and login URL (for testing).
    pub fn new(email: String, password: String, login_url: String, client: Client) -> Self {
        Self {
            client,
            email,
            password,
            login_url,
            session: Arc::new(RwLock::new(None)),
        }
    }

    /// Get a valid session cookie, auto-refreshing if expired or about to expire.
    pub async fn get_valid_session(&self) -> Result<String> {
        {
            let guard = self.session.read().await;
            if let Some(ref state) = *guard {
                if Utc::now() < state.expires_at - SESSION_REFRESH_BUFFER {
                    return Ok(state.cookie_value.clone());
                }
                warn!("Warwick session expired or expiring soon, refreshing...");
            }
        }

        let mut guard = self.session.write().await;
        if let Some(ref state) = *guard {
            if Utc::now() < state.expires_at - SESSION_REFRESH_BUFFER {
                return Ok(state.cookie_value.clone());
            }
        }

        info!("Authenticating with Warwick as {}", self.email);
        let new_session = self.perform_login().await?;
        let cookie = new_session.cookie_value.clone();
        *guard = Some(new_session);
        info!("Warwick session refreshed successfully");

        Ok(cookie)
    }

    /// Force a re-login regardless of current session state.
    /// Call this when a QR fetch returns AUTH_EXPIRED.
    pub async fn force_refresh(&self) -> Result<String> {
        warn!("Forcing Warwick session refresh due to auth failure");
        let mut guard = self.session.write().await;
        let new_session = self.perform_login().await?;
        let cookie = new_session.cookie_value.clone();
        *guard = Some(new_session);
        Ok(cookie)
    }

    async fn perform_login(&self) -> Result<SessionState> {
        let form = [
            ("email", self.email.as_str()),
            ("password", self.password.as_str()),
        ];

        let response = self.client
            .post(&self.login_url)
            .form(&form)
            .header("Content-Type", "application/x-www-form-urlencoded")
            .send()
            .await
            .context("Failed to send login request to Warwick")?;

        let status = response.status();
        let headers = response.headers().clone();

        if status == reqwest::StatusCode::OK {
            let body = response.text().await.unwrap_or_default();
            if is_login_page(&body) {
                return Err(anyhow!(
                    "Warwick login returned 200 OK but with login page HTML. \
                     Check WARWICK_EMAIL and WARWICK_PASSWORD credentials."
                ));
            }
        }

        let cookie_value = extract_session_cookie_from_headers(&headers)?;
        let now = Utc::now();

        Ok(SessionState {
            cookie_value,
            obtained_at: now,
            expires_at: now + Duration::minutes(15),
        })
    }
}

/// Detect if an HTML response body is the Warwick login page
/// rather than the authenticated dashboard.
pub fn is_login_page(body: &str) -> bool {
    body.contains("idg-box-login-primary")
        || body.contains("idg-btn-sumbit")
        || (body.contains("<title>WarWick</title>")
            && body.contains("Forgot Password?")
            && body.contains("name=\"password\""))
}

/// Extract ASP.NET_SessionId value from response Set-Cookie headers.
fn extract_session_cookie_from_headers(
    headers: &reqwest::header::HeaderMap,
) -> Result<String> {
    for header_value in headers.get_all(reqwest::header::SET_COOKIE) {
        let cookie_str = header_value.to_str().unwrap_or("");
        if let Ok(c) = cookie::Cookie::parse(cookie_str) {
            if c.name() == SESSION_COOKIE_NAME {
                return Ok(c.value().to_string());
            }
        }
        if cookie_str.starts_with(&format!("{SESSION_COOKIE_NAME}=")) {
            let value = cookie_str
                .strip_prefix(&format!("{SESSION_COOKIE_NAME}="))
                .unwrap_or("")
                .split(';')
                .next()
                .unwrap_or("");
            if !value.is_empty() {
                return Ok(value.to_string());
            }
        }
    }

    Err(anyhow!(
        "Warwick login response did not contain {} cookie. Headers: {:?}",
        SESSION_COOKIE_NAME,
        headers,
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::{Mock, MockServer, ResponseTemplate, matchers};

    #[test]
    fn detects_login_page_correctly() {
        let html = r#"<div class="idg-box-login-primary">
            <span>Sign In</span>
            <input name="password" />
            <a href="/admin/SignIn/ForgotPassword">Forgot Password?</a>
        </div>"#;
        assert!(is_login_page(html));
    }

    #[test]
    fn does_not_false_positive_on_dashboard() {
        let html = r#"<title>WarWick</title>
            <div id="wrapper">
                <span>Adisak Seesom</span>
                <table class="datatables">...</table>
            </div>"#;
        assert!(!is_login_page(html));
    }

    #[tokio::test]
    async fn extracts_session_cookie_from_set_cookie_header() {
        let mock_server = MockServer::start().await;
        Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(302)
                    .insert_header("Location", "/admin/Home")
                    .insert_header(
                        "Set-Cookie",
                        "ASP.NET_SessionId=abc123xyz; path=/; HttpOnly",
                    ),
            )
            .mount(&mock_server)
            .await;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .unwrap();
        let auth = WarwickAuth::new(
            "test@warwick.ac.th".into(),
            "password".into(),
            mock_server.uri(),
            client,
        );
        let result = auth.perform_login().await;
        assert!(result.is_ok(), "perform_login should succeed: {:?}", result.err());
        let session = result.unwrap();
        assert_eq!(session.cookie_value, "abc123xyz");
    }

    #[tokio::test]
    async fn returns_error_on_bad_credentials() {
        let mock_server = MockServer::start().await;
        Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_string(
                        r#"<div class="idg-box-login-primary">
                            <span>Sign In</span>
                            <span class="danger">Invalid credentials</span>
                        </div>"#,
                    ),
            )
            .mount(&mock_server)
            .await;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .unwrap();
        let auth = WarwickAuth::new(
            "bad@warwick.ac.th".into(),
            "wrong".into(),
            mock_server.uri(),
            client,
        );
        let result = auth.perform_login().await;
        assert!(result.is_err());
        let err = result.err().unwrap();
        assert!(err.to_string().contains("login page HTML"));
    }

    #[tokio::test]
    async fn get_valid_session_returns_cached_session() {
        let mock_server = MockServer::start().await;

        // Set up mock to respond with a real session + redirect
        let mock = Mock::given(matchers::method("POST"))
            .respond_with(
                ResponseTemplate::new(302)
                    .insert_header("Location", "/admin/Home")
                    .insert_header(
                        "Set-Cookie",
                        "ASP.NET_SessionId=sess123; path=/; HttpOnly",
                    ),
            );
        mock.mount(&mock_server).await;

        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .cookie_store(false)
            .build()
            .unwrap();
        let auth = WarwickAuth::new(
            "test@warwick.ac.th".into(),
            "password".into(),
            mock_server.uri(),
            client,
        );
        // First call should perform login
        let cookie1 = auth.get_valid_session().await.unwrap();
        assert_eq!(cookie1, "sess123");
        // Manually remove the mock to ensure it's only called once
        // (Second call should use cached session, never reaching the mock)
        mock_server.reset().await;
        let cookie2 = auth.get_valid_session().await.unwrap();
        assert_eq!(cookie2, "sess123");
    }
}
