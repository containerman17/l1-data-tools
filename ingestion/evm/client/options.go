package client

// Option configures the client
type Option func(*Client)

// WithReconnect enables automatic reconnection (default: true)
func WithReconnect(enabled bool) Option {
	return func(c *Client) {
		c.reconnect = enabled
	}
}

// WithBufferConfig sets custom buffer configuration
func WithBufferConfig(cfg BufferConfig) Option {
	return func(c *Client) {
		c.bufferConfig = cfg
	}
}
