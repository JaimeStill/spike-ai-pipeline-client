package client

import "time"

// SetJitter replaces c's backoff jitter source with f, which is given the backoff's cap and
// returns the wait. It is the only way to replace it: the Client exports no hook.
func SetJitter(c *Client, f func(time.Duration) time.Duration) { c.jitter = f }
