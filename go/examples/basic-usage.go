package main

import (
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	laserstream "laserstream-go-client"

	pb "github.com/rpcpool/yellowstone-grpc/examples/golang/proto"
)

func main() {
	log.SetFlags(0)
	log.Println("Starting Laserstream Go transaction subscription example...")

	// Define variables needed for pointer fields in the request
	commitmentLevel := pb.CommitmentLevel_CONFIRMED
	voteFilterBool := false
	failedFilterBool := false

	// Construct the protobuf request for transactions
	subscriptionRequest := &pb.SubscribeRequest{
		Transactions: map[string]*pb.SubscribeRequestFilterTransactions{
			"token_program_transactions": { // Descriptive key for the subscription
				AccountInclude: []string{"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"}, // Inlined account string
				Vote:           &voteFilterBool,
				Failed:         &failedFilterBool,
			},
		},
		Commitment: &commitmentLevel,
	}

	config := laserstream.LaserstreamConfig{
		Endpoint:             "",
		APIKey:               "",
		Insecure:             false,
		MaxReconnectAttempts: 0,
	}

	client := laserstream.NewClient(config)

	// Callback expects raw protobuf struct
	dataCallback := func(data *pb.SubscribeUpdate) {
		switch update := data.UpdateOneof.(type) {
		case *pb.SubscribeUpdate_Transaction:
			if update.Transaction != nil && update.Transaction.Transaction != nil && len(update.Transaction.Transaction.Signature) > 0 {
				sig := hex.EncodeToString(update.Transaction.Transaction.Signature)
				log.Printf("Transaction Update: Slot %d, Signature %s", update.Transaction.Slot, sig)
			} else {
				log.Printf("Received partial Transaction Update: Slot %d", update.Transaction.Slot)
			}
		}
	}

	errorCallback := func(err error) {
		log.Printf("Subscription error: %v", err)
	}

	log.Println("Subscribing to transactions for account:", "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	err := client.Subscribe(subscriptionRequest, dataCallback, errorCallback)
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}

	log.Println("Subscription initiated. Waiting for transaction data or termination signal...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Termination signal received. Closing client...")
	client.Close()
	time.Sleep(1 * time.Second)
	log.Println("Example finished.")
}
