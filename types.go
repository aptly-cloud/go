package aptly

import (
	"encoding/json"
	"strconv"
	"time"
)

// FlexibleInt64 can unmarshal from both string and number JSON values
type FlexibleInt64 int64

// UnmarshalJSON implements custom unmarshaling for FlexibleInt64
func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as number first
	var num int64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexibleInt64(num)
		return nil
	}

	// Try to unmarshal as string
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		num, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			// If parse fails, default to 0
			*f = 0
			return nil
		}
		*f = FlexibleInt64(num)
		return nil
	}

	// Default to 0 if neither works
	*f = 0
	return nil
}

// MarshalJSON implements custom marshaling for FlexibleInt64
func (f FlexibleInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(f))
}

// Int64 returns the int64 value
func (f FlexibleInt64) Int64() int64 {
	return int64(f)
}

// ConnectionState represents the current state of the WebSocket connection
type ConnectionState string

const (
	StateDisconnected    ConnectionState = "disconnected"
	StateConnecting      ConnectionState = "connecting"
	StateAuthenticating  ConnectionState = "authenticating"
	StateConnected       ConnectionState = "connected"
	StateReconnecting    ConnectionState = "reconnecting"
	StateFailed          ConnectionState = "failed"
	StateUnauthorized    ConnectionState = "unauthorized"
)

// Config holds the client configuration
type Config struct {
	// APIKey is required for authentication
	APIKey string

	// Verbose enables detailed logging
	Verbose bool

	// Endpoint is the WebSocket server URL (default: wss://connect.aptly.cloud)
	Endpoint string

	// ReconnectTimeout is the initial reconnection delay (default: 3s)
	ReconnectTimeout time.Duration
}

// MessageMetadata contains metadata about a received message
type MessageMetadata struct {
	// Offset is the message offset in the channel
	Offset int64 `json:"offset"`

	// Timestamp is when the message was sent
	Timestamp int64 `json:"timestamp"`

	// Replay indicates if this is a replayed message
	Replay bool `json:"replay"`

	// Channel is the channel name
	Channel string `json:"channel"`
}

// MessageHandler is a callback function for handling incoming messages
type MessageHandler func(data interface{}, metadata MessageMetadata)

// ConnectivityCallback is a callback for connectivity status changes
type ConnectivityCallback func(status ConnectionState)

// ChannelStats contains statistics about a channel
type ChannelStats struct {
	Name             string          `json:"name"`
	StreamHandlers   int             `json:"streamHandlers"`
	ConnectionState  ConnectionState `json:"connectionState"`
}

// ConnectionStats contains statistics about the connection
type ConnectionStats struct {
	State            ConnectionState `json:"state"`
	ClientID         string          `json:"clientId"`
	IsAuthenticated  bool            `json:"isAuthenticated"`
	ReconnectAttempts int            `json:"reconnectAttempts"`
	QueuedMessages   int             `json:"queuedMessages"`
	LastPongTime     *time.Time      `json:"lastPongTime,omitempty"`
}

// Stats contains overall SDK statistics
type Stats struct {
	Connection      ConnectionStats         `json:"connection"`
	Channels        map[string]ChannelStats `json:"channels"`
	ActiveChannels  int                     `json:"activeChannels"`
}

type HeartbeatPayload struct {
	Type string `json:"type"`
}

// Internal message types for WebSocket communication
type wsMessage struct {
	Type       string        			`json:"type"`
	Channel    string        			`json:"channel,omitempty"`
	Message    interface{}   			`json:"message,omitempty"`
	Data       interface{}   			`json:"data,omitempty"`
	Timestamp  FlexibleInt64 			`json:"timestamp,omitempty"`
	Offset     FlexibleInt64 			`json:"offset,omitempty"`
	Replay     bool          			`json:"replay,omitempty"`
	APIKey     string        			`json:"apiKey,omitempty"`
	FromOffset FlexibleInt64 			`json:"fromOffset,omitempty"`
	Error      string        			`json:"error,omitempty"`
	Payload		 *HeartbeatPayload	`json:"payload,omitempty"`
}
