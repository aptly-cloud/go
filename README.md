# Aptly Go SDK

A fault-tolerant real-time pub/sub SDK for Go, providing WebSocket-based messaging with automatic reconnection, message queuing, and authentication.

## Features

- 🔌 **WebSocket Connectivity** - Real-time bidirectional communication
- 🔄 **Automatic Reconnection** - Exponential backoff strategy for resilient connections
- 🔐 **Authentication** - Secure API key-based authentication
- 📬 **Message Queuing** - Offline message queuing with automatic flushing
- 💓 **Heartbeat Monitoring** - Connection health checks
- 📊 **Channel Management** - Multiple channels with independent subscriptions
- 🔍 **Statistics & Monitoring** - Real-time connection and channel statistics
- ⚡ **Goroutine-Safe** - Thread-safe operations throughout

## Installation

```bash
go get github.com/aptly-cloud/go
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/aptly-cloud/go"
)

func main() {
    // Create client
    client, err := aptly.NewClient(aptly.Config{
        APIKey:  "your-api-key",
        Verbose: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Cleanup()

    // Connect to server
    if err := client.Connect(); err != nil {
        log.Fatal(err)
    }

    // Create channel
    channel, err := client.Realtime().Channel("my-channel")
    if err != nil {
        log.Fatal(err)
    }

    // Subscribe to messages
    unsubscribe, err := channel.Stream(func(data interface{}, metadata aptly.MessageMetadata) {
        fmt.Printf("Received: %v\n", data)
    })
    if err != nil {
        log.Fatal(err)
    }
    defer unsubscribe()

    // Publish a message
    channel.Publish(map[string]string{
        "text": "Hello, Aptly!",
    })

    // Keep running...
    select {}
}
```

## Configuration

### Config Options

```go
type Config struct {
    // APIKey is required for authentication (required)
    APIKey string

    // Verbose enables detailed logging (default: false)
    Verbose bool

    // Endpoint is the WebSocket server URL (default: wss://connect.aptly.cloud)
    Endpoint string

    // ReconnectTimeout is the initial reconnection delay (default: 3s)
    ReconnectTimeout time.Duration
}
```

## Usage Examples

### 1. Connecting to Server

```go
client, err := aptly.NewClient(aptly.Config{
    APIKey:  "your-api-key",
    Verbose: true,
})
if err != nil {
    log.Fatal(err)
}

// Connect
if err := client.Connect(); err != nil {
    log.Fatal(err)
}

// Check connection status
fmt.Println("Connected:", client.IsConnected())
fmt.Println("Client ID:", client.GetClientID())
fmt.Println("Status:", client.GetConnectionStatus())
```

### 2. Subscribing to Messages

```go
// Create channel
channel, err := client.Realtime().Channel("notifications")
if err != nil {
    log.Fatal(err)
}

// Subscribe with message handler
unsubscribe, err := channel.Stream(func(data interface{}, metadata aptly.MessageMetadata) {
    fmt.Printf("Channel: %s\n", metadata.Channel)
    fmt.Printf("Offset: %d\n", metadata.Offset)
    fmt.Printf("Timestamp: %d\n", metadata.Timestamp)
    fmt.Printf("Replay: %v\n", metadata.Replay)
    fmt.Printf("Data: %v\n", data)
})
if err != nil {
    log.Fatal(err)
}

// Later: unsubscribe when done
defer unsubscribe()
```

### 3. Publishing Messages (WebSocket)

```go
channel, _ := client.Realtime().Channel("notifications")

// Publish a message via WebSocket
message := map[string]interface{}{
    "type": "notification",
    "text": "New message!",
    "timestamp": time.Now().Unix(),
}

if err := channel.Publish(message); err != nil {
    log.Printf("Failed to publish: %v", err)
}

// Alternative: using Send (alias)
channel.Send(message)
```

### 4. Publishing via HTTP (Fallback)

```go
// Useful when WebSocket connection is not available
response, err := client.PublishHTTP("notifications", map[string]string{
    "text": "Message via HTTP",
})
if err != nil {
    log.Fatal(err)
}

fmt.Println("Response:", response)
```

### 5. Monitoring Connectivity

```go
// Subscribe to connectivity changes
unsubscribe := client.OnConnectivityChange(func(status aptly.ConnectionState) {
    fmt.Printf("Connection status changed to: %s\n", status)

    switch status {
    case aptly.StateConnected:
        fmt.Println("✓ Connected to server")
    case aptly.StateReconnecting:
        fmt.Println("⟳ Reconnecting...")
    case aptly.StateDisconnected:
        fmt.Println("✗ Disconnected")
    case aptly.StateFailed:
        fmt.Println("✗ Connection failed")
    }
})
defer unsubscribe()
```

### 6. Multiple Channels

```go
// Create multiple channels
channel1, _ := client.Realtime().Channel("channel-1")
channel2, _ := client.Realtime().Channel("channel-2")

// Subscribe to both
unsub1, _ := channel1.Stream(func(data interface{}, metadata aptly.MessageMetadata) {
    fmt.Println("Channel 1:", data)
})

unsub2, _ := channel2.Stream(func(data interface{}, metadata aptly.MessageMetadata) {
    fmt.Println("Channel 2:", data)
})

defer unsub1()
defer unsub2()

// Publish to different channels
channel1.Publish("Message to channel 1")
channel2.Publish("Message to channel 2")
```

### 7. Statistics and Monitoring

```go
// Get overall statistics
stats := client.GetStats()

fmt.Printf("Connection State: %s\n", stats.Connection.State)
fmt.Printf("Client ID: %s\n", stats.Connection.ClientID)
fmt.Printf("Is Authenticated: %v\n", stats.Connection.IsAuthenticated)
fmt.Printf("Active Channels: %d\n", stats.ActiveChannels)
fmt.Printf("Queued Messages: %d\n", stats.Connection.QueuedMessages)
fmt.Printf("Reconnect Attempts: %d\n", stats.Connection.ReconnectAttempts)

// Get channel-specific stats
for name, channelStats := range stats.Channels {
    fmt.Printf("Channel %s: %d handlers\n", name, channelStats.StreamHandlers)
}

// Get active channel names
channels := client.GetActiveChannels()
fmt.Println("Active channels:", channels)
```

### 8. Graceful Shutdown

```go
// Set up signal handling
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

// Wait for signal
<-sigChan

// Cleanup and disconnect
client.Cleanup()
fmt.Println("Shutdown complete")
```

## Connection States

The SDK tracks the following connection states:

- `StateDisconnected` - Not connected
- `StateConnecting` - Establishing connection
- `StateAuthenticating` - Authenticating with server
- `StateConnected` - Connected and authenticated
- `StateReconnecting` - Attempting to reconnect
- `StateFailed` - Connection failed (max retries reached)
- `StateUnauthorized` - Authentication failed

## Message Metadata

Each received message includes metadata:

```go
type MessageMetadata struct {
    Offset    int64  // Message offset in the channel
    Timestamp int64  // Message timestamp (Unix milliseconds)
    Replay    bool   // Whether this is a replayed message
    Channel   string // Channel name
}
```

## Error Handling

```go
// Connection errors
if err := client.Connect(); err != nil {
    log.Printf("Connection error: %v", err)
}

// Channel creation errors
channel, err := client.Realtime().Channel("")
if err != nil {
    log.Printf("Invalid channel name: %v", err)
}

// Publishing errors
if err := channel.Publish(data); err != nil {
    log.Printf("Publish failed: %v", err)
}

// HTTP publishing errors
response, err := client.PublishHTTP("channel", data)
if err != nil {
    log.Printf("HTTP publish failed: %v", err)
}
```

## Testing

Run the example application:

```bash
# Set your API key
export APTLY_API_KEY="your-api-key"

# Run the example
cd examples
go run main.go
```

## API Reference

### Client Methods

- `NewClient(config Config) (*Client, error)` - Create new client
- `Connect() error` - Connect to server
- `Disconnect()` - Disconnect from server
- `Cleanup()` - Clean up resources
- `Realtime() *Realtime` - Get realtime interface
- `Channel(name string) (*Channel, error)` - Create/get channel
- `IsConnected() bool` - Check connection status
- `IsAuthenticated() bool` - Check authentication status
- `GetClientID() string` - Get client ID
- `GetConnectionStatus() ConnectionState` - Get connection state
- `GetActiveChannels() []string` - Get active channel names
- `GetStats() Stats` - Get statistics
- `PublishHTTP(channel string, message interface{}) (map[string]interface{}, error)` - Publish via HTTP
- `OnConnectivityChange(callback ConnectivityCallback) func()` - Subscribe to connectivity changes

### Channel Methods

- `Stream(callback MessageHandler) (func(), error)` - Subscribe to messages
- `Unstream(callback MessageHandler)` - Unsubscribe callback
- `Publish(message interface{}) error` - Publish message via WebSocket
- `Send(message interface{}) error` - Alias for Publish
- `Close()` - Close channel
- `GetStats() ChannelStats` - Get channel statistics

## Architecture

### Components

1. **Client** - Main SDK interface
2. **Connection** - WebSocket connection manager with auto-reconnect
3. **Channel** - Pub/sub channel management
4. **Stream** - Individual message streams
5. **Backoff** - Exponential backoff for reconnection
6. **Logger** - Logging utility

### Key Features

- **Thread Safety**: All operations are goroutine-safe using mutexes
- **Message Queuing**: Messages sent while offline are queued and sent on reconnection
- **Heartbeat**: Regular heartbeat messages ensure connection health
- **Automatic Resubscription**: Channels are automatically resubscribed after reconnection
- **Offset Tracking**: Message offsets are tracked for replay capability

## Best Practices

1. **Always cleanup**: Use `defer client.Cleanup()` to ensure proper resource cleanup
2. **Handle errors**: Check errors from all SDK methods
3. **Use connectivity callbacks**: Monitor connection state for better UX
4. **Unsubscribe when done**: Call the unsubscribe function returned by `Stream()`
5. **Use context for cancellation**: Integrate with Go contexts for better control

## License

See LICENSE file for details.

## Support

For issues and questions, please visit the Aptly documentation or contact support.
