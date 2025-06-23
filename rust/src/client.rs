use crate::{LaserstreamConfig, LaserstreamError};
use async_stream::try_stream;
use futures::TryStreamExt;
use futures_channel::mpsc as futures_mpsc;
use futures_util::{sink::SinkExt, Stream, StreamExt};
use std::{pin::Pin, time::Duration};
use tokio::{sync::mpsc, time::sleep};
use tonic::Status;
use tracing::{error, instrument, warn};
use uuid;
use yellowstone_grpc_client::{ClientTlsConfig, GeyserGrpcClient};
use yellowstone_grpc_proto::geyser::{
    subscribe_update::UpdateOneof, SubscribeRequest, SubscribeRequestFilterSlots,
    SubscribeRequestPing, SubscribeUpdate,
};

const HARD_CAP_RECONNECT_ATTEMPTS: u32 = (20 * 60) / 5; // 20 mins / 5 sec interval
const FIXED_RECONNECT_INTERVAL_MS: u64 = 5000; // 5 seconds fixed interval

/// Establishes a gRPC connection, handles the subscription lifecycle,
/// and provides a stream of updates. Automatically reconnects on failure.
#[instrument(skip(config, request))]
pub fn subscribe(
    config: LaserstreamConfig,
    request: Option<SubscribeRequest>,
) -> (
    impl Stream<Item = Result<SubscribeUpdate, LaserstreamError>>,
    futures_mpsc::UnboundedSender<SubscribeRequest>,
) {
    let (additional_request_tx, mut additional_request_rx) = futures_mpsc::unbounded();
    let mut additional_request_rx = additional_request_rx.fuse();
    let stream = try_stream! {
        let mut reconnect_attempts = 0;
        let mut tracked_slot: u64 = 0;

        // Determine the effective max reconnect attempts
        let effective_max_attempts = config
            .max_reconnect_attempts
            .unwrap_or(HARD_CAP_RECONNECT_ATTEMPTS) // Default to hard cap if not set
            .min(HARD_CAP_RECONNECT_ATTEMPTS); // Enforce hard cap

        // Keep original request for reconnection attempts
        let current_request = request.clone();
        let internal_slot_sub_id = format!("internal-{}", uuid::Uuid::new_v4().to_string().split('-').next().unwrap());

        let api_key_string = config.api_key.clone();

        loop {

            let attempt_request =  current_request.clone();

            // Add slot tracking if not present
            // if attempt_request.slots.is_empty() {
            //     attempt_request.slots.insert(
            //         internal_slot_sub_id.clone(),
            //         SubscribeRequestFilterSlots::default()
            //     );
            // }

            // // On reconnection, use the last tracked slot
            // if reconnect_attempts > 0 && tracked_slot > 0 {
            //     attempt_request.from_slot = Some(tracked_slot);
            // }

            match connect_and_subscribe_once(&config, attempt_request, api_key_string.clone()).await { // Pass String API key
                Ok((sender, stream)) => {

                    // Box sender and stream here before processing
                    let mut sender: Pin<Box<dyn futures_util::Sink<SubscribeRequest, Error = futures_mpsc::SendError> + Send>> = Box::pin(sender);
                    // Ensure the boxed stream yields Result<_, tonic::Status>
                    let stream: Pin<Box<dyn Stream<Item = Result<SubscribeUpdate, tonic::Status>> + Send>> =
                        Box::pin(stream.map_err(|ystatus| {
                            // Convert yellowstone_grpc_proto::tonic::Status to tonic::Status
                            let code = tonic::Code::from_i32(ystatus.code() as i32);
                            tonic::Status::new(code, ystatus.message())
                        }));
                        let mut stream = stream.fuse();
                        loop{
                        futures::select!{
                            stream_result = stream.next() => {
                                match stream_result {
                                    Some(Ok(update)) => {
                                        // Handle ping/pong
                                        if matches!(&update.update_oneof, Some(UpdateOneof::Ping(_))) {
                                            let pong_req = SubscribeRequest {
                                                ping: Some(SubscribeRequestPing { id: 1 }),
                                                ..Default::default()
                                            };
                                            if let Err(e) = sender.send(pong_req).await {
                                                warn!(error = %e, "Failed to send pong");
                                                break;
                                            }
                                            continue;
                                        }

                                        // Always track the latest slot if the update is a Slot update
                                        if let Some(UpdateOneof::Slot(s)) = &update.update_oneof {
                                            tracked_slot = s.slot;
                                        }

                                        // Yield the update to the stream consumer
                                        yield update;
                                    }
                                    Some(Err(status)) => {
                                        warn!(error = %status, "Stream error, attempting reconnection");
                                        break;
                                    }
                                    None => {
                                        warn!("Stream ended, preparing to reconnect...");
                                        break;
                                    }
                                }
                            }
                            additional_req = additional_request_rx.next() => {
                                // println!("Additional request received: {:?}", additional_req);
                                match additional_req {
                                    Some(new_request) => {
                                        // Send the additional request through the gRPC connection
                                        // println!("Sending additional request: {:?}", new_request);
                                        if let Err(e) = sender.send(new_request).await {
                                            warn!(error = %e, "Failed to send additional request");
                                            break;
                                        }
                                        // println!("Additional request sent");
                                    }
                                    None => {
                                        // Channel closed, but continue with the stream
                                        // You might want to break here if you want to stop when no more requests can be sent
                                        warn!("Additional request channel closed");
                                    }
                                }
                            }
                        }
                    }
                    warn!("Connection loop ended, preparing to reconnect...");
                }
                Err(err) => {
                    // Error from connect_and_subscribe_once (GeyserGrpcClientError)
                    warn!(error = %err, "Failed to connect/subscribe, preparing to reconnect...");
                }
            }

            reconnect_attempts += 1;
            if reconnect_attempts >= effective_max_attempts {
                error!(attempts = effective_max_attempts, config_value = ?config.max_reconnect_attempts, "Max reconnection attempts reached");
                Err(LaserstreamError::MaxReconnectAttempts(Status::cancelled(
                    format!("Max reconnection attempts ({}) reached", effective_max_attempts)
                )))?;
            }

            // Use fixed interval delay, but only after the first failed attempt
            if reconnect_attempts > 1 {
                let delay = Duration::from_millis(FIXED_RECONNECT_INTERVAL_MS);
                sleep(delay).await;
            }
        }
    };
    (stream, additional_request_tx)
}

#[instrument(skip(config, request, api_key))]
pub async fn connect_and_subscribe_once(
    config: &LaserstreamConfig,
    request: Option<SubscribeRequest>,
    api_key: String,
) -> Result<
    (
        impl futures_util::Sink<SubscribeRequest, Error = futures_mpsc::SendError> + Send,
        impl Stream<Item = Result<SubscribeUpdate, yellowstone_grpc_proto::tonic::Status>> + Send,
    ),
    tonic::Status,
> {
    let mut builder =
        GeyserGrpcClient::build_from_shared(config.endpoint.clone()) // Use endpoint String directly
            .map_err(|e| tonic::Status::internal(format!("Build client error: {}", e)))?
            .x_token(Some(api_key))
            .map_err(|e| tonic::Status::internal(format!("Set token error: {}", e)))?
            .connect_timeout(Duration::from_secs(10))
            .max_decoding_message_size(1_000_000_000)
            .timeout(Duration::from_secs(10))
            .tls_config(ClientTlsConfig::new().with_enabled_roots())
            .map_err(|e| tonic::Status::internal(format!("TLS config error: {}", e)))?
            .connect()
            .await
            .map_err(|e| tonic::Status::internal(format!("Connect error: {}", e)))?;

    let (sender, stream) = builder
        .subscribe_with_request(request)
        .await
        .map_err(|e| tonic::Status::internal(format!("Subscribe error: {}", e)))?;

    Ok((sender, stream))
}
