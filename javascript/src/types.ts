import { SubscribeRequest, CommitmentLevel } from "@triton-one/yellowstone-grpc";

/**
 * Configuration for LaserStream client
 */
export interface LaserstreamConfig {
  /**
   * API Key from whitelisted users
   */
  apiKey: string;
  
  /**
   * Endpoint URL to connect to
   */
  endpoint: string;

  /**
   * Maximum number of reconnection attempts
   */
  maxReconnectAttempts?: number;
}

// Re-export relevant types for easier usage
export { CommitmentLevel, SubscribeRequest };