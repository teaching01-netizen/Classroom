use qr_command_center_warwick::{WarwickQrClient, WarwickAuth, FetchError, QrClient};
use wiremock::{Mock, MockServer, ResponseTemplate, matchers};

#[tokio::test]
async fn test_fetch_qr_success() {
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

    let qr_server = MockServer::start().await;
    Mock::given(matchers::method("POST"))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_string(r#"{"qrUrl":"data:image/png;base64,abcd","qrTime":60}"#)
                .insert_header("Content-Type", "application/json"),
        )
        .mount(&qr_server)
        .await;

    let client = reqwest::Client::builder()
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

    assert!(result.is_ok());
    let qr = result.unwrap();
    assert_eq!(qr.qrUrl, "data:image/png;base64,abcd");
    assert_eq!(qr.qrTime, 60);
}

#[tokio::test]
async fn test_fetch_qr_session_expired() {
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

    let qr_server = MockServer::start().await;
    Mock::given(matchers::method("POST"))
        .respond_with(ResponseTemplate::new(302).insert_header("Location", "/login"))
        .mount(&qr_server)
        .await;

    let client = reqwest::Client::builder()
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
