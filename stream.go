package aptly

import "sync"

// Stream represents an individual stream within a channel
type Stream struct {
	offset      int64
	state       string
	channelName string
	connection  *Connection
	handler     MessageHandler
	logger      *Logger
	mu          sync.RWMutex
}

// NewStream creates a new stream instance
func NewStream(channelName string, connection *Connection, offset int64, logger *Logger) *Stream {
	return &Stream{
		channelName: channelName,
		connection:  connection,
		offset:      offset,
		logger:      logger,
	}
}

// Start starts the stream with the given handler
func (s *Stream) Start(handler MessageHandler) error {
	if handler == nil {
		return ErrInvalidHandler
	}

	s.mu.Lock()
	s.handler = handler
	s.state = "streaming"
	s.mu.Unlock()

	// Send subscribe message
	subscribeMsg := wsMessage{
		Type:       "subscribe",
		Channel:    s.channelName,
		FromOffset: FlexibleInt64(s.offset),
	}

	s.connection.Send(subscribeMsg)
	s.logger.Info("Streaming started")

	return nil
}

// Stop stops the stream
func (s *Stream) Stop() {
	s.mu.Lock()
	s.state = "stopped"
	s.mu.Unlock()
}

// GetStatus returns the stream status
func (s *Stream) GetStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	connState := s.connection.GetState()
	if connState != StateConnected {
		return string(connState)
	}

	return s.state
}

// HandleMessage calls the stream handler with the message
func (s *Stream) HandleMessage(data interface{}, metadata MessageMetadata) {
	s.mu.RLock()
	handler := s.handler
	s.mu.RUnlock()

	if handler != nil {
		handler(data, metadata)
	}
}

// Common errors
var (
	ErrInvalidHandler = &AptlyError{Code: "INVALID_HANDLER", Message: "Stream callback must be a function"}
)

// AptlyError represents an SDK error
type AptlyError struct {
	Code    string
	Message string
}

func (e *AptlyError) Error() string {
	return e.Message
}
