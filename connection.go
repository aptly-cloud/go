package aptly

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Connection manages WebSocket connections with automatic reconnection
type Connection struct {
	config              Config
	ws                  *websocket.Conn
	state               ConnectionState
	clientID            string
	logger              *Logger
	backoff             *ExponentialBackoff
	shouldReconnect     bool
	messageHandlers     []func(wsMessage)
	messageQueue        []queuedMessage
	maxQueueSize        int
	lastPongTime        *time.Time
	heartbeatInterval   time.Duration
	isAuthenticated     bool
	authTimeout         time.Duration
	connectivityChan    chan ConnectionState
	stopHeartbeat       chan struct{}
	reconnectTimer      *time.Timer
	mu                  sync.RWMutex
	connectMu           sync.Mutex
	connectPromise      chan error
	authPromise         chan error
	done                chan struct{}
	readDone            chan struct{}
}

type queuedMessage struct {
	message   wsMessage
	timestamp time.Time
}

// NewConnection creates a new connection instance
func NewConnection(config Config, connectivityChan chan ConnectionState, logger *Logger) *Connection {
	clientID := generateClientID()

	backoffConfig := BackoffConfig{
		InitialDelay: config.ReconnectTimeout,
		Multiplier:   1.5,
		MaxDelay:     30 * time.Second,
		MaxAttempts:  20,
	}

	if backoffConfig.InitialDelay == 0 {
		backoffConfig.InitialDelay = 3 * time.Second
	}

	return &Connection{
		config:            config,
		state:             StateDisconnected,
		clientID:          clientID,
		logger:            logger,
		backoff:           NewExponentialBackoff(backoffConfig, logger),
		messageHandlers:   make([]func(wsMessage), 0),
		messageQueue:      make([]queuedMessage, 0),
		maxQueueSize:      10000,
		heartbeatInterval: 30 * time.Second,
		authTimeout:       30 * time.Second,
		connectivityChan:  connectivityChan,
		done:              make(chan struct{}),
	}
}

// Connect establishes WebSocket connection with retry logic
func (c *Connection) Connect() error {
	c.connectMu.Lock()

	// If already connecting, wait for that to complete
	if c.connectPromise != nil {
		c.logger.Info("Already connecting, waiting...")
		promise := c.connectPromise
		c.connectMu.Unlock()
		err := <-promise
		return err
	}

	c.shouldReconnect = true
	c.connectPromise = make(chan error, 1)
	promise := c.connectPromise

	go func() {
		err := c.attemptConnection()
		c.connectMu.Lock()
		if c.connectPromise != nil {
			c.connectPromise <- err
			close(c.connectPromise)
			c.connectPromise = nil
		}
		c.connectMu.Unlock()
	}()

	// Unlock before waiting to avoid deadlock
	c.connectMu.Unlock()

	err := <-promise
	return err
}

// attemptConnection attempts to establish WebSocket connection
func (c *Connection) attemptConnection() error {
	c.setState(StateConnecting)

	// Create WebSocket connection with Origin header
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	// Construct Origin header from endpoint
	origin := c.getOriginFromEndpoint()
	headers := make(map[string][]string)
	headers["Origin"] = []string{origin}

	ws, _, err := dialer.Dial(c.config.Endpoint, headers)
	if err != nil {
		c.logger.Errorf("Failed to connect: %v", err)
		return err
	}

	c.mu.Lock()
	c.ws = ws
	c.readDone = make(chan struct{})
	c.mu.Unlock()

	c.setState(StateAuthenticating)
	c.backoff.Reset()

	// Start message reader goroutine BEFORE authentication
	// so it can receive auth response messages
	go c.readMessages()

	// Start authentication
	if err := c.startAuthentication(); err != nil {
		c.logger.Errorf("Authentication failed: %v", err)
		ws.Close()
		c.setState(StateUnauthorized)
		return err
	}

	c.setState(StateConnected)

	// Start heartbeat
	c.startHeartbeat()

	// Flush queued messages
	c.flushMessageQueue()

	return nil
}

// getOriginFromEndpoint converts WebSocket endpoint to HTTP origin
func (c *Connection) getOriginFromEndpoint() string {
	endpoint := c.config.Endpoint

	// Convert ws:// to http:// and wss:// to https://
	if len(endpoint) >= 6 && endpoint[:6] == "wss://" {
		return "https://" + endpoint[6:]
	} else if len(endpoint) >= 5 && endpoint[:5] == "ws://" {
		return "http://" + endpoint[5:]
	}

	// If already http(s), return as is
	return endpoint
}

// Disconnect closes the WebSocket connection
func (c *Connection) Disconnect() {
	c.mu.Lock()
	c.shouldReconnect = false
	c.isAuthenticated = false

	// Stop reconnect timer if any
	if c.reconnectTimer != nil {
		c.reconnectTimer.Stop()
		c.reconnectTimer = nil
	}

	// Stop heartbeat
	if c.stopHeartbeat != nil {
		close(c.stopHeartbeat)
		c.stopHeartbeat = nil
	}

	// Close WebSocket
	if c.ws != nil {
		c.ws.Close()
		c.ws = nil
	}

	// Signal done
	select {
	case <-c.done:
		// Already closed
	default:
		close(c.done)
		c.done = make(chan struct{}) // Reset for potential reconnection
	}

	c.mu.Unlock()

	c.setState(StateDisconnected)
}

// Send sends a message through the WebSocket connection
func (c *Connection) Send(message wsMessage) {
	if !c.IsConnected() {
		c.queueMessage(message)
		return
	}

	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()

	if ws == nil {
		c.queueMessage(message)
		return
	}

	data, err := json.Marshal(message)
	if err != nil {
		c.logger.Errorf("Failed to marshal message: %v", err)
		return
	}

	err = ws.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		c.logger.Errorf("Failed to send message: %v", err)
		c.queueMessage(message)
	}
}

// AddMessageHandler adds a message handler
func (c *Connection) AddMessageHandler(handler func(wsMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messageHandlers = append(c.messageHandlers, handler)
}

// RemoveMessageHandler removes a message handler
func (c *Connection) RemoveMessageHandler(handler func(wsMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Note: Function comparison in Go is tricky, consider using IDs if needed
	// For now, we'll keep this simple
}

// GetState returns the current connection state
func (c *Connection) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// IsConnected checks if the connection is established and authenticated
func (c *Connection) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state == StateConnected && c.isAuthenticated && c.ws != nil
}

// GetClientID returns the client ID
func (c *Connection) GetClientID() string {
	return c.clientID
}

// GetStats returns connection statistics
func (c *Connection) GetStats() ConnectionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return ConnectionStats{
		State:             c.state,
		ClientID:          c.clientID,
		IsAuthenticated:   c.isAuthenticated,
		ReconnectAttempts: c.backoff.GetAttempts(),
		QueuedMessages:    len(c.messageQueue),
		LastPongTime:      c.lastPongTime,
	}
}

// readMessages reads messages from WebSocket in a loop
func (c *Connection) readMessages() {
	defer func() {
		c.mu.Lock()
		if c.readDone != nil {
			close(c.readDone)
		}
		c.mu.Unlock()
	}()

	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()

	for {
		if ws == nil {
			return
		}

		_, messageData, err := ws.ReadMessage()
		if err != nil {
			c.logger.Infof("WebSocket read error: %v", err)
			c.handleDisconnection()
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(messageData, &msg); err != nil {
			c.logger.Errorf("Failed to parse message: %v", err)
			continue
		}

		c.handleMessage(msg)
	}
}

// handleMessage handles incoming WebSocket messages
func (c *Connection) handleMessage(msg wsMessage) {
	// Handle authentication messages
	if !c.isAuthenticated {
		c.handleAuthMessage(msg)
		return
	}

	// Handle heartbeat
	if msg.Type == "heartbeat" {
		now := time.Now()
		c.mu.Lock()
		c.lastPongTime = &now
		c.mu.Unlock()
		return
	}

	// Forward to message handlers
	c.mu.RLock()
	handlers := make([]func(wsMessage), len(c.messageHandlers))
	copy(handlers, c.messageHandlers)
	c.mu.RUnlock()

	for _, handler := range handlers {
		go func(h func(wsMessage)) {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Errorf("Message handler panic: %v", r)
				}
			}()
			h(msg)
		}(handler)
	}
}

// startAuthentication starts the authentication process
func (c *Connection) startAuthentication() error {
	c.authPromise = make(chan error, 1)

	// Start auth timeout
	authTimer := time.NewTimer(c.authTimeout)
	defer authTimer.Stop()

	// Send auth credentials
	c.sendAuthCredentials()

	select {
	case err := <-c.authPromise:
		return err
	case <-authTimer.C:
		return errors.New("authentication timeout")
	}
}

// sendAuthCredentials sends authentication credentials
func (c *Connection) sendAuthCredentials() {
	authMsg := wsMessage{
		Type:      "auth",
		APIKey:    c.config.APIKey,
		Timestamp: FlexibleInt64(time.Now().UnixMilli()),
	}

	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()

	if ws == nil {
		if c.authPromise != nil {
			c.authPromise <- errors.New("connection closed")
		}
		return
	}

	data, _ := json.Marshal(authMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Errorf("Failed to send auth: %v", err)
		if c.authPromise != nil {
			c.authPromise <- err
		}
	}
}

// handleAuthMessage handles authentication-related messages
func (c *Connection) handleAuthMessage(msg wsMessage) {
	switch msg.Type {
	case "auth_required":
		c.sendAuthCredentials()

	case "auth_success":
		// Wait for connected message

	case "connected":
		c.mu.Lock()
		c.isAuthenticated = true
		c.mu.Unlock()

		if c.authPromise != nil {
			c.authPromise <- nil
			close(c.authPromise)
			c.authPromise = nil
		}

	case "auth_timeout", "error":
		err := fmt.Errorf("auth error: %s", msg.Error)
		if c.authPromise != nil {
			c.authPromise <- err
			close(c.authPromise)
			c.authPromise = nil
		}

	case "heartbeat":
		now := time.Now()
		c.mu.Lock()
		c.lastPongTime = &now
		c.mu.Unlock()
	}
}

// handleDisconnection handles connection loss
func (c *Connection) handleDisconnection() {
	c.mu.Lock()
	c.isAuthenticated = false
	shouldReconnect := c.shouldReconnect
	c.mu.Unlock()

	if !shouldReconnect {
		c.setState(StateDisconnected)
		return
	}

	if c.backoff.IsMaxAttemptsReached() {
		c.setState(StateFailed)
		return
	}

	c.setState(StateReconnecting)
	c.scheduleReconnection()
}

// scheduleReconnection schedules the next reconnection attempt
func (c *Connection) scheduleReconnection() {
	delay := c.backoff.GetDelay()

	c.mu.Lock()
	c.reconnectTimer = time.AfterFunc(delay, func() {
		if c.shouldReconnect && c.GetState() == StateReconnecting {
			if err := c.attemptConnection(); err != nil {
				c.logger.Errorf("Reconnection attempt failed: %v", err)
				if !c.backoff.IsMaxAttemptsReached() {
					c.scheduleReconnection()
				} else {
					c.setState(StateFailed)
				}
			}
		}
	})
	c.mu.Unlock()
}

// setState sets the connection state and notifies listeners
func (c *Connection) setState(newState ConnectionState) {
	c.mu.Lock()
	oldState := c.state
	c.state = newState
	c.mu.Unlock()

	if oldState != newState && c.connectivityChan != nil {
		select {
		case c.connectivityChan <- newState:
		default:
			// Non-blocking send
		}
	}
}

// queueMessage queues a message for later sending
func (c *Connection) queueMessage(msg wsMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.messageQueue) >= c.maxQueueSize {
		// Remove oldest message
		c.messageQueue = c.messageQueue[1:]
	}

	c.messageQueue = append(c.messageQueue, queuedMessage{
		message:   msg,
		timestamp: time.Now(),
	})
}

// flushMessageQueue sends all queued messages
func (c *Connection) flushMessageQueue() {
	c.mu.Lock()
	queue := make([]queuedMessage, len(c.messageQueue))
	copy(queue, c.messageQueue)
	c.messageQueue = c.messageQueue[:0]
	c.mu.Unlock()

	for _, qm := range queue {
		c.Send(qm.message)
	}
}

// startHeartbeat starts the heartbeat monitoring
func (c *Connection) startHeartbeat() {
	c.mu.Lock()
	if c.stopHeartbeat != nil {
		close(c.stopHeartbeat)
	}
	c.stopHeartbeat = make(chan struct{})
	stopChan := c.stopHeartbeat
	now := time.Now()
	c.lastPongTime = &now
	c.mu.Unlock()

	ticker := time.NewTicker(c.heartbeatInterval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !c.IsConnected() {
					return
				}

				// Check for heartbeat timeout
				c.mu.RLock()
				lastPong := c.lastPongTime
				c.mu.RUnlock()

				if lastPong != nil && time.Since(*lastPong) > c.heartbeatInterval*2 {
					c.logger.Warn("Heartbeat timeout detected, closing connection")
					c.mu.RLock()
					ws := c.ws
					c.mu.RUnlock()
					if ws != nil {
						ws.Close()
					}
					return
				}

				// Send heartbeat
				c.Send(wsMessage{
					Type:      "heartbeat",
					Timestamp: FlexibleInt64(time.Now().UnixMilli()),
				})

			case <-stopChan:
				return

			case <-c.done:
				return
			}
		}
	}()
}

// generateClientID generates a unique client ID
func generateClientID() string {
	return fmt.Sprintf("client_%d_%d", time.Now().UnixNano(), time.Now().Unix()%1000000)
}
