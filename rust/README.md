## Rust

# helius-laserstream

Rust client for Laserstream. Features automatic reconnection with slot tracking - if connection is lost, the client automatically reconnects and continues streaming from the last processed slot, ensuring no data is missed.

## Installation

Add the dependency to your `Cargo.toml`:

```toml
[dependencies]
helius-laserstream = "0.0.1" # Or the latest version
futures-util = "0.3"
futures = "0.3"
tokio = { version = "1", features = ["rt-multi-thread", "macros"] }
```

Or use `cargo add`:

```bash
cargo add helius-laserstream
cargo add futures-util
cargo add futures
cargo add tokio --features rt-multi-thread,macros
```

## Usage Example

```rust
use futures_util::StreamExt;
use helius_laserstream::{
    grpc::{
        SubscribeRequest,
        SubscribeRequestFilterTransactions,
    },
    subscribe, LaserstreamConfig,
};
use std::collections::HashMap;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let config = LaserstreamConfig {
        api_key: "your-api-key".to_string(),
        endpoint: "your-endpoint-url".parse()?,
        ..Default::default()
    };

    // --- Subscription Request ---
    // Subscribe to all confirmed non-vote transactions involving the Token program
    let mut token_transactions_filter = HashMap::new();
    token_transactions_filter.insert(
        "client".to_string(), 
        SubscribeRequestFilterTransactions {
            account_include: vec!["TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA".to_string()],
            vote: Some(false),
            failed: Some(false),
            ..Default::default()
        },
    );

    let request = SubscribeRequest {
        transactions: token_transactions_filter,
        ..Default::default()
    };

    // --- Subscribe and Process ---
    println!("Connecting and subscribing...");
    let stream = subscribe(config, request);

    // Pin the stream to the stack
    futures::pin_mut!(stream);

    while let Some(result) = stream.next().await {
        match result {
            Ok(update) => {
                // Simple printing for the example
                println!("Received update: {:?}", update);
            }
            Err(e) => {
                eprintln!("Stream error: {}", e);
            }
        }
    }

    println!("Stream finished.");
    Ok(())
}
```

## License

MIT
