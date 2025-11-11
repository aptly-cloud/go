package aptly

import (
	"errors"
	"sync"
)

// streamEntry holds a stream and its associated callback
type streamEntry struct {
	callback MessageHandler
	stream   *Stream
}

// Channel represents a pub/sub channel with real-time messaging capabilities
type Channel struct {
	name           string
	connection     *Connection
	logger         *Logger
	streams        []*streamEntry
	messageHandler func(wsMessage)
	offset         int64
	mu             sync.RWMutex
}

// NewChannel creates a new channel instance
func NewChannel(name string, connection *Connection, logger *Logger) (*Channel, error) {
	if name == "" {
		return nil, errors.New("channel name is required and must be a string")
	}

	ch := &Channel{
		name:       name,
		connection: connection,
		logger:     logger,
		streams:    make([]*streamEntry, 0),
		offset:     0,
	}

	// Create message handler
	ch.messageHandler = func(msg wsMessage) {
		ch.handleMessage(msg)
	}

	// Subscribe to connection messages
	connection.AddMessageHandler(ch.messageHandler)

	return ch, nil
}

// Stream subscribes to messages on this channel
func (ch *Channel) Stream(callback MessageHandler) (func(), error) {
	if callback == nil {
		return nil, errors.New("stream callback must be a function")
	}

	ch.mu.Lock()
	previousSize := len(ch.streams)

	stream := NewStream(ch.name, ch.connection, ch.offset, ch.logger)
	entry := &streamEntry{
		callback: callback,
		stream:   stream,
	}
	ch.streams = append(ch.streams, entry)

	if len(ch.streams) > previousSize {
		ch.logger.Infof("Streaming started! Total streams: %d, Offset: %d", len(ch.streams), ch.offset)
	}
	ch.mu.Unlock()

	// Start the stream
	if err := stream.Start(callback); err != nil {
		ch.mu.Lock()
		// Remove the entry we just added
		for i, e := range ch.streams {
			if e.stream == stream {
				ch.streams = append(ch.streams[:i], ch.streams[i+1:]...)
				break
			}
		}
		ch.mu.Unlock()
		return nil, err
	}

	// Return unsubscribe function that captures the stream
	return func() {
		ch.unstreamByStream(stream)
	}, nil
}

// unstreamByStream removes a stream by its pointer reference
func (ch *Channel) unstreamByStream(stream *Stream) {
	ch.mu.Lock()
	previousSize := len(ch.streams)

	// Find and remove the stream entry by pointer comparison
	for i, entry := range ch.streams {
		if entry.stream == stream {
			entry.stream.Stop()
			ch.streams = append(ch.streams[:i], ch.streams[i+1:]...)

			if previousSize > len(ch.streams) {
				ch.logger.Infof("Unstreaming success! Total streams: %d", len(ch.streams))
			}
			break
		}
	}

	// If no more streams, close the channel
	if len(ch.streams) == 0 {
		ch.logger.Info("No streams left, closing the channel")
		ch.mu.Unlock()
		ch.Close()
		return
	}
	ch.mu.Unlock()
}

// Unstream unsubscribes a specific callback from the channel
// Deprecated: Use the unsubscribe function returned by Stream() instead
func (ch *Channel) Unstream(callback MessageHandler) {
	// This method is kept for backward compatibility but won't work reliably
	// since function comparison is not supported in Go
	// Instead, users should use the unsubscribe function returned by Stream()
	ch.mu.Lock()
	if len(ch.streams) > 0 {
		// Just remove the first stream as a fallback
		entry := ch.streams[0]
		entry.stream.Stop()
		ch.streams = ch.streams[1:]
		ch.logger.Infof("Unstreaming success! Total streams: %d", len(ch.streams))
	}

	// If no more streams, close the channel
	if len(ch.streams) == 0 {
		ch.logger.Info("No streams left, closing the channel")
		ch.mu.Unlock()
		ch.Close()
		return
	}
	ch.mu.Unlock()
}

// Publish publishes a message to the channel via WebSocket
func (ch *Channel) Publish(message interface{}) error {
	if !ch.connection.IsConnected() {
		return errors.New("connection is not established")
	}

	publishMsg := wsMessage{
		Type:      "publish",
		Channel:   ch.name,
		Message:   message,
		Timestamp: 0, // Will be set by connection
	}

	ch.connection.Send(publishMsg)
	return nil
}

// Send is an alias for Publish for backward compatibility
func (ch *Channel) Send(message interface{}) error {
	return ch.Publish(message)
}

// Close unsubscribes from the channel and stops receiving messages
func (ch *Channel) Close() {
	unsubscribeMsg := wsMessage{
		Type:    "unsubscribe",
		Channel: ch.name,
	}

	ch.connection.Send(unsubscribeMsg)
}

// GetStats returns channel statistics
func (ch *Channel) GetStats() ChannelStats {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	return ChannelStats{
		Name:            ch.name,
		StreamHandlers:  len(ch.streams),
		ConnectionState: ch.connection.GetState(),
	}
}

// handleMessage handles incoming WebSocket messages for this channel
func (ch *Channel) handleMessage(msg wsMessage) {
	ch.mu.RLock()
	streamCount := len(ch.streams)
	ch.mu.RUnlock()

	// If no streams are active, do not process the message
	if streamCount == 0 {
		ch.logger.Infof("No streams active, skipping message: %v", msg)
		return
	}

	// Handle subscription confirmation
	if msg.Type == "subscribed" && msg.Channel == ch.name {
		ch.logger.Infof("Subscribed to channel: %s", ch.name)
		return
	}

	// Handle unsubscription confirmation
	if msg.Type == "unsubscribed" && msg.Channel == ch.name {
		ch.logger.Infof("Unsubscribed from channel: %s", ch.name)
		return
	}

	// Handle incoming messages
	if msg.Type == "message" {
		if msg.Channel == "" || msg.Channel == ch.name {
			// Add channel to message if missing
			if msg.Channel == "" {
				msg.Channel = ch.name
			}
			ch.processIncomingMessage(msg)
			return
		}
	}

	// Handle publish confirmation
	if msg.Type == "published" && msg.Channel == ch.name {
		ch.logger.Info("Message published to channel")
	}
}

// processIncomingMessage processes an incoming message and forwards to stream handlers
func (ch *Channel) processIncomingMessage(msg wsMessage) {
	// Create metadata
	metadata := MessageMetadata{
		Offset:    msg.Offset.Int64(),
		Timestamp: msg.Timestamp.Int64(),
		Replay:    msg.Replay,
		Channel:   msg.Channel,
	}

	// Update offset if needed
	msgOffset := msg.Offset.Int64()
	if msgOffset > 0 {
		ch.mu.Lock()
		if msgOffset > ch.offset {
			ch.offset = msgOffset
		}
		ch.mu.Unlock()
	}

	// Forward to all stream handlers
	ch.mu.RLock()
	streams := make([]*Stream, 0, len(ch.streams))
	for _, entry := range ch.streams {
		streams = append(streams, entry.stream)
	}
	ch.mu.RUnlock()

	for _, stream := range streams {
		go func(s *Stream) {
			defer func() {
				if r := recover(); r != nil {
					ch.logger.Errorf("Stream handler panic: %v", r)
				}
			}()
			s.HandleMessage(msg.Data, metadata)
		}(stream)
	}
}

// Resubscribe re-subscribes to the channel after reconnection
func (ch *Channel) Resubscribe() {
	ch.mu.RLock()
	streamCount := len(ch.streams)
	ch.mu.RUnlock()

	if streamCount > 0 {
		// Re-subscribe logic can be added here if needed
		ch.logger.Info("Resubscribing to channel:", ch.name)
	}
}
