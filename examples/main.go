package main

import (
	"fmt"
	"log"
	"time"

	aptly "github.com/aptly-cloud/go"
)

func main() {
	fmt.Println("\n=== Aptly Go SDK Example ===\n")

	// Create client configuration
	config := aptly.Config{
		APIKey:  "", // Enter your Server API Key here. Get it at https://console.aptly.cloud
	}

	// 1. Initialize Client
	client, err := aptly.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	fmt.Println("✓ Client initialized successfully")

	// Setting up connectivity monitoring
	unsubscribe := client.OnConnectivityChange(func(status aptly.ConnectionState) {
		fmt.Printf("  → Status: %s\n", status)
	})

	defer unsubscribe()

	fmt.Println("\nConnecting to Aptly server...")
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// 2. Select a channel
	channel, err := client.Realtime().Channel("my-channel")
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}

	// Subscribe to the channel
	fmt.Println("\nSubscribing to 'my-channel'...")

	// Channel to signal when message is received
	done := make(chan bool)

	// 3. Subscribe to the channel
	unsubscribeChannel, err := channel.Stream(func(data interface{}, metadata aptly.MessageMetadata) {
		if metadata.Replay == false {
			fmt.Println("\n📬 New message received!")

			if dataMap, ok := data.(map[string]interface{}); ok {
				if text, ok := dataMap["text"].(string); ok {
					fmt.Printf("   Message: %s\n", text)
				}
				if timestamp, ok := dataMap["timestamp"].(float64); ok {
					t := time.Unix(int64(timestamp), 0)
					fmt.Printf("   Received at: %s\n", t.Format("2006-01-02 15:04:05"))
				}
			}

			// Signal that we received a message
			done <- true
		}
	})

	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}

	defer unsubscribeChannel()

	fmt.Println("✓ Successfully subscribed to 'my-channel'")

	// 4. Publish a message to the channel
	fmt.Println("\nPublishing a test message...")
	message := map[string]interface{}{
		"text": "Hello, Aptly from Go SDK!",
		"timestamp": time.Now().Unix(),
	}

	if err := channel.Publish(message); err != nil {
		log.Printf("Failed to publish message: %v", err)
	} else {
		fmt.Println("✓ Message published successfully")
	}

	fmt.Println("\nWaiting for messages...")

	// Wait for message to be received
	<-done

	fmt.Println("\n=== Example completed ===\n")
}
