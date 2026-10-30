use axum::{Router, routing::get};

#[tokio::main]
async fn main() {
    let port = std::env::var("PORT").unwrap_or_else(|_| "8080".into());
    let app = Router::new().route("/", get(|| async { "hello from axum" }));
    let listener = tokio::net::TcpListener::bind(("0.0.0.0", port.parse::<u16>().unwrap())).await.unwrap();
    axum::serve(listener, app)
        .with_graceful_shutdown(async { tokio::signal::ctrl_c().await.ok(); })
        .await
        .unwrap();
}
