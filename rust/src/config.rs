#[derive(Debug, Clone)]
pub struct LaserstreamConfig {
    /// API Key for authentication.
    pub api_key: String,
    /// The Laserstream endpoint URL.
    pub endpoint: String,
    /// Maximum number of reconnection attempts. Defaults to 10.
    /// A hard cap of 240 attempts (20 minutes / 5 seconds) is enforced internally.
    pub max_reconnect_attempts: Option<u32>,
}

impl Default for LaserstreamConfig {
    fn default() -> Self {
        Self {
            api_key: String::new(),
            endpoint: String::new(),
            max_reconnect_attempts: None, // Default to None
        }
    }
}

impl LaserstreamConfig {
    pub fn new(endpoint: String, api_key: String) -> Self {
        Self {
            endpoint,
            api_key,
            max_reconnect_attempts: None, // Default to None
        }
    }

    /// Sets the maximum number of reconnection attempts.
    pub fn with_max_reconnect_attempts(mut self, attempts: u32) -> Self {
        self.max_reconnect_attempts = Some(attempts);
        self
    }
}
