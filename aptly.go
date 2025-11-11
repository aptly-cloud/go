// Package aptly provides a real-time pub/sub SDK for the Aptly platform
package aptly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Client is the main SDK client for Aptly real-time pub/sub functionality
type Client struct {
	config              Config
	logger              *Logger
	connection          *Connection
	channels            map[string]*Channel
	connectivityChan    chan ConnectionState
	connectivityHandlers []ConnectivityCallback
	mu                  sync.RWMutex
	stopConnectivity    chan struct{}
}

// Realtime provides the realtime interface for channel management
type Realtime struct {
	client *Client
}

// NewClient creates a new Aptly client instance
func NewClient(config Config) (*Client, error) {
	if config.APIKey == "" {
		return nil, errors.New("API key is required")
	}

	// Set defaults
	if config.Endpoint == "" {
		config.Endpoint = "wss://connect.aptly.cloud"
	}

	logger := NewLogger(config.Verbose)

	// Create connectivity channel
	connectivityChan := make(chan ConnectionState, 100)

	// Create connection
	connection := NewConnection(config, connectivityChan, logger)

	client := &Client{
		config:           config,
		logger:           logger,
		connection:       connection,
		channels:         make(map[string]*Channel),
		connectivityChan: connectivityChan,
		stopConnectivity: make(chan struct{}),
	}

	// Set up connection handlers
	client.setupConnectionHandlers()

	logger.Info("Client initialized")

	return client, nil
}

// Connect establishes connection to the WebSocket server
func (c *Client) Connect() error {
	c.logger.Info("Connecting to server")

	if err := c.connection.Connect(); err != nil {
		c.logger.Errorf("Failed to connect: %v", err)
		return err
	}

	c.logger.Info("Connected to server")

	// Re-subscribe to existing channels after reconnection
	c.resubscribeChannels()

	return nil
}

// Disconnect disconnects from the WebSocket server and cleans up resources
func (c *Client) Disconnect() {
	c.mu.Lock()

	// Close all channels
	for _, channel := range c.channels {
		channel.Close()
	}
	c.channels = make(map[string]*Channel)

	c.mu.Unlock()

	// Disconnect the connection
	c.connection.Disconnect()

	// Stop connectivity monitoring
	close(c.stopConnectivity)
}

// Realtime returns the realtime interface for channel management
func (c *Client) Realtime() *Realtime {
	return &Realtime{client: c}
}

// Channel creates or retrieves a channel instance for the given channel name
func (r *Realtime) Channel(name string) (*Channel, error) {
	return r.client.Channel(name)
}

// Channel creates or retrieves a channel instance for the given channel name
func (c *Client) Channel(name string) (*Channel, error) {
	if name == "" {
		return nil, errors.New("channel name is required and must be a string")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return existing channel if already created
	if ch, exists := c.channels[name]; exists {
		return ch, nil
	}

	// Create new channel instance
	channel, err := NewChannel(name, c.connection, c.logger)
	if err != nil {
		return nil, err
	}

	c.channels[name] = channel

	return channel, nil
}

// GetConnectionStatus returns the current connection status
func (c *Client) GetConnectionStatus() ConnectionState {
	return c.connection.GetState()
}

// IsAuthenticated checks if the connection is authenticated
func (c *Client) IsAuthenticated() bool {
	return c.connection.isAuthenticated
}

// GetClientID returns the client ID for this connection
func (c *Client) GetClientID() string {
	return c.connection.GetClientID()
}

// GetActiveChannels returns all active channel names
func (c *Client) GetActiveChannels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	channels := make([]string, 0, len(c.channels))
	for name := range c.channels {
		channels = append(channels, name)
	}

	return channels
}

// GetStats returns statistics for all channels and the connection
func (c *Client) GetStats() Stats {
	c.mu.RLock()
	channelStats := make(map[string]ChannelStats)
	for name, channel := range c.channels {
		channelStats[name] = channel.GetStats()
	}
	activeChannels := len(c.channels)
	c.mu.RUnlock()

	return Stats{
		Connection:     c.connection.GetStats(),
		Channels:       channelStats,
		ActiveChannels: activeChannels,
	}
}

// PublishHTTP publishes a message to a channel via HTTP API (fallback method)
func (c *Client) PublishHTTP(channel string, message interface{}) (map[string]interface{}, error) {
	if channel == "" || message == nil {
		return nil, errors.New("channel and message are required")
	}

	// Convert WebSocket endpoint to HTTP
	httpEndpoint := c.config.Endpoint
	if httpEndpoint[:6] == "wss://" {
		httpEndpoint = "https://" + httpEndpoint[6:]
	} else if httpEndpoint[:5] == "ws://" {
		httpEndpoint = "http://" + httpEndpoint[5:]
	}

	url := httpEndpoint + "/realtime"

	// Create request body
	body := map[string]interface{}{
		"channel": channel,
		"message": message,
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.config.APIKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP publish failed: %d %s - %s", resp.StatusCode, resp.Status, string(bodyBytes))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// IsConnected checks if the client is currently connected to the server
func (c *Client) IsConnected() bool {
	return c.connection.IsConnected()
}

// OnConnectivityChange subscribes to connectivity status changes
func (c *Client) OnConnectivityChange(callback ConnectivityCallback) func() {
	if callback == nil {
		c.logger.Error("Connectivity callback must be a function")
		return func() {}
	}

	c.mu.Lock()
	c.connectivityHandlers = append(c.connectivityHandlers, callback)
	c.logger.Info("Connectivity listener added")
	c.mu.Unlock()

	// Return unsubscribe function
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		// Remove the callback
		for i, cb := range c.connectivityHandlers {
			// Function comparison is tricky in Go, but we'll try
			if &cb == &callback {
				c.connectivityHandlers = append(c.connectivityHandlers[:i], c.connectivityHandlers[i+1:]...)
				c.logger.Info("Connectivity listener removed")
				break
			}
		}
	}
}

// Cleanup removes all event listeners and cleans up resources
func (c *Client) Cleanup() {
	c.Disconnect()

	c.mu.Lock()
	c.connectivityHandlers = nil
	c.mu.Unlock()
}

// setupConnectionHandlers sets up connection event handlers
func (c *Client) setupConnectionHandlers() {
	// Start goroutine to listen for connectivity changes
	go func() {
		for {
			select {
			case status := <-c.connectivityChan:
				// Re-subscribe channels when connection is re-established
				if status == StateConnected {
					c.resubscribeChannels()
				}

				// Notify all connectivity handlers
				c.mu.RLock()
				handlers := make([]ConnectivityCallback, len(c.connectivityHandlers))
				copy(handlers, c.connectivityHandlers)
				c.mu.RUnlock()

				for _, handler := range handlers {
					go handler(status)
				}

			case <-c.stopConnectivity:
				return
			}
		}
	}()
}

// resubscribeChannels re-subscribes all active channels after reconnection
func (c *Client) resubscribeChannels() {
	c.mu.RLock()
	channels := make([]*Channel, 0, len(c.channels))
	for _, channel := range c.channels {
		channels = append(channels, channel)
	}
	c.mu.RUnlock()

	for _, channel := range channels {
		channel.Resubscribe()
	}
}
