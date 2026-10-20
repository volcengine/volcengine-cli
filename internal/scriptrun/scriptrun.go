// Package scriptrun executes multi-API orchestration scripts.
//
// A script runs in a worker (local CPython today, a hosted sandbox later).
// The supported way for a script to call cloud APIs is call_api, whose
// requests are brokered over the frame protocol in protocol.go; this is a
// contract, not enforced isolation. The broker owns limits, cancellation and
// the call journal; an Invoker owns identity, credentials and policy for each
// individual API request.
//
// The local worker is trusted code execution under the current OS user. It is
// not a security sandbox: the process can read files, open sockets and start
// processes with the user's privileges.
package scriptrun

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Call statuses recorded in the journal.
const (
	CallStatusOK       = "ok"       // the API returned a successful response
	CallStatusFailed   = "failed"   // the API returned an error response
	CallStatusRejected = "rejected" // the request was never sent
	CallStatusUnknown  = "unknown"  // the request may have reached the API; its outcome is unknown
)

// Run statuses.
const (
	RunStatusSucceeded     = "succeeded"
	RunStatusFailed        = "failed"
	RunStatusCancelled     = "cancelled"
	RunStatusTimedOut      = "timed_out"
	RunStatusAborted       = "aborted"
	RunStatusLimitExceeded = "limit_exceeded" // the call budget was exhausted and the script kept calling
)

// Broker error codes returned to scripts.
const (
	ErrorCodeCallLimitExceeded = "CallLimitExceeded"
	ErrorCodeCallTimeout       = "CallTimeout"
	ErrorCodeRunCancelled      = "RunCancelled"
	ErrorCodeInvalidRequest    = "InvalidRequest"
)

// Call is one API request issued by a script.
type Call struct {
	Seq        int
	Service    string
	API        string
	Region     string
	Parameters map[string]interface{}
}

// CallError describes a rejected, failed or unknown-outcome API call.
type CallError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	RequestID string `json:"request_id,omitempty"`
	// Fatal aborts the whole run after this call, e.g. when the bound
	// identity changed. It is not sent to the worker.
	Fatal bool `json:"-"`
}

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// Invoker executes one API request on behalf of a run. Implementations must
// enforce identity binding and authorization on every request and classify
// the outcome: rejected when nothing was sent, failed for API error
// responses, and unknown when the request may have reached the API.
type Invoker interface {
	Invoke(ctx context.Context, call Call) (map[string]interface{}, *CallError)
}

// Request is a script run request.
type Request struct {
	Code   string
	Args   json.RawMessage
	Limits Limits
}

// ScriptError is an exception raised by the script itself.
type ScriptError struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Traceback string `json:"traceback,omitempty"`
}

// JournalEntry records one call_api request in issue order.
type JournalEntry struct {
	Seq        int    `json:"seq"`
	Service    string `json:"service"`
	API        string `json:"api"`
	Region     string `json:"region,omitempty"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	StartedMS  int64  `json:"started_ms"`
	DurationMS int64  `json:"duration_ms"`
}

// Summary counts journal entries by status.
type Summary struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Failed   int `json:"failed"`
	Rejected int `json:"rejected"`
	Unknown  int `json:"unknown"`
}

// Call budget: a run may issue at most Limits.MaxCalls API calls. The first
// call beyond the budget is rejected with CallLimitExceeded and journaled so
// the script can stop gracefully; any further call ends the run with
// RunStatusLimitExceeded. The journal therefore never exceeds MaxCalls+1
// entries, and calls that already completed stay in it.

// Result is the outcome of one run. Calls that completed before a failure,
// cancellation or timeout stay in the journal; a run is never replayed.
type Result struct {
	Status          string          `json:"status"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *ScriptError    `json:"error,omitempty"`
	Output          string          `json:"output"`
	OutputTruncated bool            `json:"output_truncated,omitempty"`
	Calls           []JournalEntry  `json:"calls"`
	Summary         Summary         `json:"summary"`
	DurationMS      int64           `json:"duration_ms"`
}

// Executor runs a script and proxies its API calls to an Invoker. The local
// CPython executor implements it; a hosted sandbox adapter can implement the
// same interface by forwarding the frame protocol to its worker.
type Executor interface {
	Execute(ctx context.Context, request Request, invoker Invoker) (*Result, error)
}

// Limits bound one run.
type Limits struct {
	MaxCalls       int           `json:"max_calls"`
	MaxConcurrency int           `json:"max_concurrency"`
	Timeout        time.Duration `json:"-"`
	CallTimeout    time.Duration `json:"-"`
	MaxOutputBytes int           `json:"max_output_bytes"`
	MaxResultBytes int           `json:"max_result_bytes"`
	MaxFrameBytes  int           `json:"max_frame_bytes"`
	MaxCodeBytes   int           `json:"max_code_bytes"`
}

// DefaultLimits returns the limits used when a caller does not override them.
func DefaultLimits() Limits {
	return Limits{
		MaxCalls:       100,
		MaxConcurrency: 4,
		Timeout:        120 * time.Second,
		CallTimeout:    30 * time.Second,
		MaxOutputBytes: 64 * 1024,
		MaxResultBytes: 1024 * 1024,
		MaxFrameBytes:  4 * 1024 * 1024,
		MaxCodeBytes:   256 * 1024,
	}
}

// MaxLimits are the upper bounds a caller may request.
func MaxLimits() Limits {
	return Limits{
		MaxCalls:       1000,
		MaxConcurrency: 16,
		Timeout:        15 * time.Minute,
		CallTimeout:    5 * time.Minute,
		MaxOutputBytes: 1024 * 1024,
		MaxResultBytes: 4 * 1024 * 1024,
		MaxFrameBytes:  8 * 1024 * 1024,
		MaxCodeBytes:   1024 * 1024,
	}
}

// Validate checks that every limit is positive and within MaxLimits.
func (l Limits) Validate() error {
	maximum := MaxLimits()
	checks := []struct {
		name         string
		value, upper int64
	}{
		{"max_calls", int64(l.MaxCalls), int64(maximum.MaxCalls)},
		{"max_concurrency", int64(l.MaxConcurrency), int64(maximum.MaxConcurrency)},
		{"max_output_bytes", int64(l.MaxOutputBytes), int64(maximum.MaxOutputBytes)},
		{"max_result_bytes", int64(l.MaxResultBytes), int64(maximum.MaxResultBytes)},
		{"max_frame_bytes", int64(l.MaxFrameBytes), int64(maximum.MaxFrameBytes)},
		{"max_code_bytes", int64(l.MaxCodeBytes), int64(maximum.MaxCodeBytes)},
	}
	for _, check := range checks {
		if check.value <= 0 || check.value > check.upper {
			return fmt.Errorf("%s must be positive and at most %d", check.name, check.upper)
		}
	}
	if l.Timeout <= 0 || l.Timeout > maximum.Timeout {
		return fmt.Errorf("timeout must be positive and at most %s", maximum.Timeout)
	}
	if l.CallTimeout <= 0 || l.CallTimeout > maximum.CallTimeout {
		return fmt.Errorf("call_timeout must be positive and at most %s", maximum.CallTimeout)
	}
	if l.MaxResultBytes > l.MaxFrameBytes {
		return fmt.Errorf("max_result_bytes must not exceed max_frame_bytes")
	}
	return nil
}
