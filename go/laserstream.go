package laserstream

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"sync"
	"time"

	pb "github.com/rpcpool/yellowstone-grpc/examples/golang/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// LaserstreamConfig holds the configuration for the client.
type LaserstreamConfig struct {
	Endpoint             string
	APIKey               string
	Insecure             bool
	MaxReconnectAttempts int // 0 or negative uses default max (~240 attempts over 20min)
}

// DataCallback defines the function signature for handling received data.
type DataCallback func(data *pb.SubscribeUpdate)

// ErrorCallback defines the function signature for handling errors.
type ErrorCallback func(err error)

// Client manages the connection and subscription to Laserstream.
type Client struct {
	config             LaserstreamConfig
	conn               *grpc.ClientConn
	stream             pb.Geyser_SubscribeClient
	lastSlot           uint64
	mu                 sync.Mutex
	cancel             context.CancelFunc
	running            bool
	dataCallback       DataCallback
	errorCallback      ErrorCallback
	subRequest         *pb.SubscribeRequest // Stores the initial request (or modified if internal slot sub added)
	hasInternalSlotSub bool                 // Flag if we added an internal slot sub

}

// NewClient creates a new Laserstream client instance.
func NewClient(config LaserstreamConfig) *Client {
	return &Client{
		config: config,
	}
}

// Subscribe initiates a subscription to the Laserstream service.
func (c *Client) Subscribe(
	req *pb.SubscribeRequest,
	dataCallback DataCallback,
	errorCallback ErrorCallback,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("client is already subscribed")
	}

	// Clone the request so we don't modify the user's original
	initialReq := proto.Clone(req).(*pb.SubscribeRequest)

	// Add internal slot subscription if user didn't provide one
	if len(initialReq.Slots) == 0 {
		c.hasInternalSlotSub = true
		internalSlotSubID := fmt.Sprintf("internal_slot_%d", rand.Intn(1000000))
		if initialReq.Slots == nil {
			initialReq.Slots = make(map[string]*pb.SubscribeRequestFilterSlots)
		}
		initialReq.Slots[internalSlotSubID] = &pb.SubscribeRequestFilterSlots{}
	} else {
		c.hasInternalSlotSub = false
	}

	c.subRequest = initialReq
	c.dataCallback = dataCallback
	c.errorCallback = errorCallback

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			cancel()
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	geyserClient := pb.NewGeyserClient(c.conn)

	streamCtx := ctx
	md := metadata.New(map[string]string{"x-token": c.config.APIKey})
	streamCtx = metadata.NewOutgoingContext(streamCtx, md)

	stream, err := geyserClient.Subscribe(streamCtx)
	if err != nil {
		cancel()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		return fmt.Errorf("failed to create subscribe stream: %w", err)
	}
	c.stream = stream

	if err := c.stream.Send(c.subRequest); err != nil {
		cancel()
		c.stream.CloseSend()
		c.stream = nil
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		return fmt.Errorf("failed to send subscription request: %w", err)
	}

	c.running = true
	go c.receiveLoop(ctx)

	return nil
}

// Close terminates the subscription and closes the connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.stream != nil {
		c.stream.CloseSend()
		c.stream = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.running = false
}

// --- Helper functions ---

// connect establishes a gRPC connection.
// Assumes the caller holds the client mutex (c.mu).
func (c *Client) connect(ctx context.Context) error {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	endpoint := c.config.Endpoint
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("error parsing endpoint: %w", err)
	}

	// Hardcode port 443 as per previous discussions
	endpoint = u.Hostname() + ":" + "443"

	insecureDial := c.config.Insecure
	var opts []grpc.DialOption
	if insecureDial {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		creds := credentials.NewClientTLSFromCert(nil, "")
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}
	opts = append(opts, grpc.WithKeepaliveParams(kacp))

	conn, err := grpc.DialContext(ctx, endpoint, opts...)
	if err != nil {
		return err
	}

	c.conn = conn
	return nil
}

// receiveLoop runs in a goroutine, receiving messages and handling reconnects.
func (c *Client) receiveLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.running = false
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		if c.stream != nil {
			c.stream.CloseSend()
			c.stream = nil
		}
		c.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		stream := c.stream
		c.mu.Unlock()

		if stream == nil {
			if !c.attemptReconnect(ctx) {
				return
			}
			continue
		}

		resp, err := stream.Recv()

		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c.mu.Lock()
			if c.errorCallback != nil {
				c.errorCallback(err)
			}
			c.stream = nil
			c.mu.Unlock()

			st, ok := status.FromError(err)
			if (ok && (st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded)) || err == io.EOF {
				if !c.attemptReconnect(ctx) {
					return
				}
			} else {
				return // Non-recoverable error
			}
		} else {
			suppressCallback := false

			if slotUpdate, ok := resp.UpdateOneof.(*pb.SubscribeUpdate_Slot); ok {
				if slotUpdate.Slot != nil {
					newSlot := slotUpdate.Slot.Slot
					if newSlot > 0 {
						c.mu.Lock()
						c.lastSlot = newSlot
						c.mu.Unlock()
					}
				}
				if c.hasInternalSlotSub {
					suppressCallback = true
				}
			}

			c.mu.Lock()
			callback := c.dataCallback
			c.mu.Unlock()
			if callback != nil && !suppressCallback {
				callback(resp)
			}
		}
	}
}

// attemptReconnect handles the logic for reconnecting.
func (c *Client) attemptReconnect(ctx context.Context) bool {
	const reconnectInterval = 5 * time.Second
	const maxReconnectWindow = 20 * time.Minute
	maxPossibleAttempts := int(maxReconnectWindow / reconnectInterval)
	if maxPossibleAttempts < 1 {
		maxPossibleAttempts = 1
	}

	c.mu.Lock()
	userRequestedAttempts := c.config.MaxReconnectAttempts
	c.mu.Unlock()

	attemptsToMake := userRequestedAttempts
	if attemptsToMake <= 0 {
		attemptsToMake = maxPossibleAttempts
	} else if attemptsToMake > maxPossibleAttempts {
		attemptsToMake = maxPossibleAttempts
	}

	for currentAttempt := 1; currentAttempt <= attemptsToMake; currentAttempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		select {
		case <-time.After(reconnectInterval):
			c.mu.Lock()
			if ctx.Err() != nil {
				c.mu.Unlock()
				return false
			}
			apiKey := c.config.APIKey
			subReq := c.subRequest // Use the initial request (potentially with internal slot sub)
			lastKnownSlot := c.lastSlot
			errorCb := c.errorCallback
			c.mu.Unlock()

			if err := c.connect(ctx); err != nil {
				errMsg := fmt.Sprintf("Reconnect connection failed on attempt %d/%d: %v", currentAttempt, attemptsToMake, err)
				if errorCb != nil {
					errorCb(fmt.Errorf("%s", errMsg))
				}
				continue
			}

			c.mu.Lock()
			geyserClient := pb.NewGeyserClient(c.conn)
			streamCtx := ctx
			if apiKey != "" { // Check if API key exists before adding metadata
				md := metadata.New(map[string]string{"x-token": apiKey})
				streamCtx = metadata.NewOutgoingContext(streamCtx, md)
			}
			errorCb = c.errorCallback

			resubReq := proto.Clone(subReq).(*pb.SubscribeRequest)

			// Set FromSlot for replay if we have a last known slot
			if lastKnownSlot > 0 {
				// Using the FromSlot field (field 11) provided in the proto definition
				resubReq.FromSlot = &lastKnownSlot
			} else {
				// If no slot tracked yet, ensure FromSlot is nil (which proto.Clone should handle)
				resubReq.FromSlot = nil
			}

			// Note: The internal slot subscription (if added) remains in the resubReq.
			// This ensures slot tracking continues even if the user only subscribed
			// to non-slot types initially. It becomes slightly redundant for replay
			// once FromSlot is active, but guarantees tracking.

			stream, err := geyserClient.Subscribe(streamCtx)
			if err != nil {
				errMsg := fmt.Sprintf("Failed to re-create stream on attempt %d/%d: %v", currentAttempt, attemptsToMake, err)
				if c.conn != nil {
					c.conn.Close()
					c.conn = nil
				}
				c.mu.Unlock()
				if errorCb != nil {
					errorCb(fmt.Errorf("%s", errMsg))
				}
				continue
			}

			if err := stream.Send(resubReq); err != nil {
				errMsg := fmt.Sprintf("Failed to re-send subscription request on attempt %d/%d: %v", currentAttempt, attemptsToMake, err)
				stream.CloseSend()
				if c.conn != nil {
					c.conn.Close()
					c.conn = nil
				}
				c.mu.Unlock()
				if errorCb != nil {
					errorCb(fmt.Errorf("%s", errMsg))
				}
				continue
			}

			c.stream = stream
			c.mu.Unlock()
			return true // Reconnect successful

		case <-ctx.Done():
			return false
		}
	}

	maxAttemptsErr := fmt.Errorf("failed to reconnect after %d attempts", attemptsToMake)
	c.mu.Lock()
	errorCb := c.errorCallback
	c.mu.Unlock()
	if errorCb != nil {
		errorCb(maxAttemptsErr)
	}
	return false
}

var kacp = keepalive.ClientParameters{
	Time:                10 * time.Second,
	Timeout:             time.Second,
	PermitWithoutStream: true,
}
