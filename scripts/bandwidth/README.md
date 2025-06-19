# Bandwidth Tester

A common issue when working with gRPC subscriptions is not having enough bandwidth to handle the data stream. This tool helps you test the maximum bandwidth your connection can support against any gRPC server.

## Usage

1. **Navigate to the directory:**
   ```sh
   cd scripts/bandwidth
   ```

2. **Run the bandwidth tester:**
   ```sh
   cargo run --release -- --laserstream-url <LS_URL> --api-key <API_KEY>
   ```
   - Replace `<LS_URL>` with your Laserstream gRPC endpoint URL.
   - Replace `<API_KEY>` with your API key.

## Example

```sh
cargo run --release -- --laserstream_url https://your-grpc-endpoint.com --api_key your_api_key_here
```

## Notes

- This tool is intended for benchmarking and diagnostics.
- Make sure your API key and endpoint are valid and have sufficient permissions.