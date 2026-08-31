package cmd

import (
	"context"
	"errors"
	"net"
	"time"
)

const consoleDeviceCodeDefaultInterval = 5 * time.Second
const consoleDeviceCodeSlowDownIncrement = 5 * time.Second

// consoleDeviceCodeMaxPollInterval caps timeout backoff so a doubled interval
// cannot grow past the token HTTP timeout. RFC 8628 recommends doubling after
// a connection timeout but does not require an unbounded wait.
const consoleDeviceCodeMaxPollInterval = consoleTokenRequestTimeout

// consoleDeviceCodeMaxTransientErrors bounds how many consecutive transient
// failures (network blips, decode errors, server_error) the poll loop tolerates
// before giving up. Transient errors within this budget do not abort the login,
// so a still-valid device code keeps polling per RFC 8628 instead of forcing the
// user to restart on a momentary hiccup.
const consoleDeviceCodeMaxTransientErrors = 5

// deviceCodePollControl owns RFC 8628 poll backpressure: interval, slow_down,
// timeout doubling, and the transient-error budget.
type deviceCodePollControl struct {
	interval        time.Duration
	transientErrors int
}

func newDeviceCodePollControl(intervalSeconds int) deviceCodePollControl {
	interval := time.Duration(intervalSeconds) * time.Second
	if interval <= 0 {
		interval = consoleDeviceCodeDefaultInterval
	}
	return deviceCodePollControl{interval: interval}
}

// handleTokenError updates backpressure after a failed token poll.
// A nil return means the caller should keep polling.
func (c *deviceCodePollControl) handleTokenError(err error) error {
	code, ok := consoleOAuthErrorCode(err)
	if !ok {
		if noteErr := c.noteTransient(err); noteErr != nil {
			return noteErr
		}
		if isConsoleDeviceCodePollTimeout(err) {
			c.interval = nextConsoleDeviceCodePollInterval(c.interval)
		}
		return nil
	}
	switch code {
	case "authorization_pending":
		c.transientErrors = 0
		return nil
	case "slow_down":
		c.transientErrors = 0
		c.interval += consoleDeviceCodeSlowDownIncrement
		return nil
	case "access_denied":
		return trErrorf("device authorization was denied")
	case "expired_token", "invalid_device_code":
		return trErrorf("device code is invalid or expired; please run 've login' again")
	case "server_error", "temporarily_unavailable":
		return c.noteTransient(err)
	default:
		return trErrorf("polling device authorization token: %w", err)
	}
}

func (c *deviceCodePollControl) noteTransient(err error) error {
	c.transientErrors++
	if c.transientErrors > consoleDeviceCodeMaxTransientErrors {
		return trErrorf("polling device authorization token: %w", err)
	}
	return nil
}

func consoleOAuthErrorCode(err error) (string, bool) {
	var apiErr *ConsoleOAuthAPIError
	if !errors.As(err, &apiErr) {
		return "", false
	}
	return apiErr.Response.Error, apiErr.Response.Error != ""
}

func isConsoleDeviceCodePollTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func nextConsoleDeviceCodePollInterval(current time.Duration) time.Duration {
	if current <= 0 {
		current = consoleDeviceCodeDefaultInterval
	}
	next := current * 2
	if next > consoleDeviceCodeMaxPollInterval || next <= current {
		return consoleDeviceCodeMaxPollInterval
	}
	return next
}
