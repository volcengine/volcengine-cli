package scriptrun

import (
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed bootstrap.py
var bootstrapSource string

const (
	// cancelGrace lets the script's finally blocks run after a cancel frame
	// before the process tree is killed.
	cancelGrace = 500 * time.Millisecond
	// callDrainTimeout bounds how long the broker waits for in-flight calls to
	// observe cancellation before recording them as unknown.
	callDrainTimeout = 10 * time.Second
	outputDrainTime  = time.Second
	frameOverhead    = 1024
	callStatusActive = "pending"
)

// LocalExecutor runs scripts in a local CPython worker process. It is trusted
// code execution with the current user's privileges, not a sandbox.
type LocalExecutor struct {
	// Python is the absolute path of a validated interpreter.
	Python string
}

// WorkerArguments returns the interpreter arguments for the local worker.
// -I ignores PYTHON* variables and user site-packages and keeps the working
// directory off sys.path; -S skips site; -B avoids writing bytecode.
func WorkerArguments() []string {
	return []string{"-I", "-S", "-B", "-u", "-X", "utf8", "-c", bootstrapSource}
}

// Execute implements Executor.
func (e *LocalExecutor) Execute(ctx context.Context, request Request, invoker Invoker) (*Result, error) {
	if e == nil || strings.TrimSpace(e.Python) == "" {
		return nil, fmt.Errorf("python interpreter is not configured")
	}
	if invoker == nil {
		return nil, fmt.Errorf("API invoker is not configured")
	}
	limits := request.Limits
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	if len(request.Code) > limits.MaxCodeBytes {
		return nil, fmt.Errorf("code is %d bytes, exceeding max_code_bytes %d", len(request.Code), limits.MaxCodeBytes)
	}
	if !utf8.ValidString(request.Code) {
		return nil, fmt.Errorf("code must be valid UTF-8")
	}
	args := request.Args
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	if !json.Valid(args) {
		return nil, fmt.Errorf("args must be valid JSON")
	}
	startFrame, err := encodeFrame(startMessage{Type: "start", Protocol: ProtocolVersion, Code: request.Code, Args: args})
	if err != nil {
		return nil, err
	}
	if len(startFrame) > limits.MaxFrameBytes {
		return nil, fmt.Errorf("code and args exceed max_frame_bytes %d", limits.MaxFrameBytes)
	}

	workDir, err := os.MkdirTemp("", "ve-script-")
	if err != nil {
		return nil, fmt.Errorf("create script working directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	command := exec.Command(e.Python, WorkerArguments()...)
	command.Dir = workDir
	command.Env = workerEnvironment(workDir)
	prepareProcessTree(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	started := time.Now()
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Python worker: %w", err)
	}
	tree, err := attachProcessTree(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	run := &localRun{
		parent: ctx, limits: limits, invoker: invoker, command: command, tree: tree,
		stdin: stdin, stdout: stdout, stderr: stderr, started: started,
		output: &limitedBuffer{limit: limits.MaxOutputBytes},
	}
	return run.execute(startFrame), nil
}

type frameResult struct {
	body []byte
	err  error
}

type completion struct {
	index int
	frame []byte
	entry JournalEntry
	fatal *CallError
}

type localRun struct {
	parent  context.Context
	limits  Limits
	invoker Invoker
	command *exec.Cmd
	tree    *processTree
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
	started time.Time
	output  *limitedBuffer

	journal []JournalEntry
}

func (r *localRun) execute(startFrame []byte) *Result {
	result := &Result{}
	stopped := make(chan struct{})
	runCtx, cancelCalls := context.WithCancel(r.parent)
	defer cancelCalls()

	outputDone := make(chan struct{})
	go func() {
		r.output.drain(r.stderr)
		close(outputDone)
	}()
	frames := make(chan frameResult, 1)
	go func() {
		for {
			body, err := readFrame(r.stdout, r.limits.MaxFrameBytes)
			select {
			case frames <- frameResult{body: body, err: err}:
			case <-stopped:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	writes := make(chan []byte, 16)
	go func() {
		for frame := range writes {
			if _, err := r.stdin.Write(frame); err != nil {
				for range writes {
				}
				return
			}
		}
	}()
	send := func(frame []byte) {
		select {
		case writes <- frame:
		case <-stopped:
		}
	}

	send(startFrame)
	timer := time.NewTimer(r.limits.Timeout)
	defer timer.Stop()
	completions := make(chan completion, r.limits.MaxCalls)
	semaphore := make(chan struct{}, r.limits.MaxConcurrency)
	var calls sync.WaitGroup
	issued := 0
	workerExited, workerFinished := false, false

loop:
	for {
		select {
		case frame := <-frames:
			if frame.err != nil {
				workerExited = true
				result.Status = RunStatusFailed
				if errors.Is(frame.err, errFrameTooLarge) {
					result.Error = &ScriptError{Type: "ProtocolError", Message: fmt.Sprintf("worker frame exceeds max_frame_bytes %d", r.limits.MaxFrameBytes)}
				} else {
					result.Error = &ScriptError{Type: "WorkerExited", Message: "the Python worker exited before reporting a result"}
				}
				break loop
			}
			message, err := decodeWorkerMessage(frame.body)
			if err != nil {
				result.Status = RunStatusFailed
				result.Error = &ScriptError{Type: "ProtocolError", Message: err.Error()}
				break loop
			}
			switch message.Type {
			case "call":
				if message.ID == nil {
					result.Status = RunStatusFailed
					result.Error = &ScriptError{Type: "ProtocolError", Message: "call frame has no id"}
					break loop
				}
				issued++
				if issued > r.limits.MaxCalls+1 {
					// The over-budget rejection was already delivered once;
					// further attempts end the run so the journal and the
					// final payload stay bounded at max_calls+1 entries.
					result.Status = RunStatusLimitExceeded
					result.Error = &ScriptError{Type: ErrorCodeCallLimitExceeded, Message: fmt.Sprintf("the script kept calling after exceeding max_calls %d; the run was stopped", r.limits.MaxCalls)}
					break loop
				}
				r.startCall(runCtx, issued, *message.ID, message, semaphore, completions, &calls, send)
			case "done":
				workerFinished = true
				raw := message.Result
				if len(raw) == 0 {
					raw = json.RawMessage("null")
				}
				if len(raw) > r.limits.MaxResultBytes {
					result.Status = RunStatusFailed
					result.Error = &ScriptError{Type: "ResultTooLarge", Message: fmt.Sprintf("result is %d bytes, exceeding max_result_bytes %d", len(raw), r.limits.MaxResultBytes)}
				} else {
					result.Status = RunStatusSucceeded
					result.Result = raw
				}
				break loop
			case "error":
				workerFinished = true
				result.Status = RunStatusFailed
				result.Error = message.Error
				if result.Error == nil {
					result.Error = &ScriptError{Type: "Error", Message: "script failed"}
				}
				break loop
			default:
				result.Status = RunStatusFailed
				result.Error = &ScriptError{Type: "ProtocolError", Message: fmt.Sprintf("unknown worker frame type %q", message.Type)}
				break loop
			}
		case done := <-completions:
			r.journal[done.index] = done.entry
			send(done.frame)
			if done.fatal != nil {
				result.Status = RunStatusAborted
				result.Error = &ScriptError{Type: "RunAborted", Message: done.fatal.Error()}
				break loop
			}
		case <-r.parent.Done():
			result.Status = RunStatusCancelled
			result.Error = &ScriptError{Type: "Cancelled", Message: "the run was cancelled"}
			break loop
		case <-timer.C:
			result.Status = RunStatusTimedOut
			result.Error = &ScriptError{Type: "TimeoutError", Message: fmt.Sprintf("the run exceeded its %s wall-clock limit", r.limits.Timeout)}
			break loop
		}
	}

	if !workerFinished && !workerExited {
		// Ask the script to unwind, then give finally blocks a short grace
		// period before the process tree is killed.
		if frame, err := encodeFrame(cancelMessage{Type: "cancel"}); err == nil {
			select {
			case writes <- frame:
			default:
			}
		}
		r.waitForWorkerExit(frames, cancelGrace)
	}
	cancelCalls()
	r.drainCalls(&calls, completions)
	close(stopped)
	r.tree.kill()
	select {
	case <-outputDone:
	case <-time.After(outputDrainTime):
	}
	close(writes)
	_ = r.stdin.Close()
	waitErr := r.command.Wait()
	r.tree.close()
	if workerExited && result.Error != nil && result.Error.Type == "WorkerExited" && waitErr != nil {
		result.Error.Message += ": " + waitErr.Error()
	}

	result.Output, result.OutputTruncated = r.output.text()
	result.Calls = r.journal
	if result.Calls == nil {
		result.Calls = []JournalEntry{}
	}
	result.Summary = summarize(result.Calls)
	result.DurationMS = time.Since(r.started).Milliseconds()
	return result
}

func (r *localRun) startCall(runCtx context.Context, seq int, id int64, message *workerMessage, semaphore chan struct{}, completions chan<- completion, calls *sync.WaitGroup, send func([]byte)) {
	entry := JournalEntry{
		Seq: seq, Service: message.Service, API: message.API, Region: message.Region,
		StartedMS: time.Since(r.started).Milliseconds(),
	}
	if seq > r.limits.MaxCalls {
		callErr := &CallError{Code: ErrorCodeCallLimitExceeded, Status: CallStatusRejected, Message: fmt.Sprintf("the run already issued max_calls %d API calls", r.limits.MaxCalls)}
		applyCallError(&entry, callErr)
		r.journal = append(r.journal, entry)
		frame, _ := resultFrame(id, seq, nil, callErr, r.limits.MaxFrameBytes)
		send(frame)
		return
	}
	entry.Status = callStatusActive
	index := len(r.journal)
	r.journal = append(r.journal, entry)
	call := Call{Seq: seq, Service: message.Service, API: message.API, Region: message.Region, Parameters: message.Parameters}
	if call.Parameters == nil {
		call.Parameters = map[string]interface{}{}
	}
	calls.Add(1)
	go func() {
		defer calls.Done()
		began := time.Now()
		finish := func(data map[string]interface{}, callErr *CallError) {
			done := entry
			done.DurationMS = time.Since(began).Milliseconds()
			if callErr != nil {
				applyCallError(&done, callErr)
			} else {
				done.Status = CallStatusOK
				done.RequestID = responseRequestID(data)
			}
			frame, replaced := resultFrame(id, seq, data, callErr, r.limits.MaxFrameBytes)
			if replaced && callErr == nil {
				done.ErrorCode = "ResponseTooLarge"
				done.Error = "the API succeeded but its response exceeds max_frame_bytes and was not delivered to the script"
			}
			var fatal *CallError
			if callErr != nil && callErr.Fatal {
				fatal = callErr
			}
			completions <- completion{index: index, frame: frame, entry: done, fatal: fatal}
		}
		select {
		case semaphore <- struct{}{}:
		case <-runCtx.Done():
			finish(nil, &CallError{Code: ErrorCodeRunCancelled, Status: CallStatusRejected, Message: "the run ended before this call was sent"})
			return
		}
		defer func() { <-semaphore }()
		if runCtx.Err() != nil {
			finish(nil, &CallError{Code: ErrorCodeRunCancelled, Status: CallStatusRejected, Message: "the run ended before this call was sent"})
			return
		}
		callCtx, cancel := context.WithTimeout(runCtx, r.limits.CallTimeout)
		data, callErr := r.invoker.Invoke(callCtx, call)
		if callErr != nil && callErr.Status != CallStatusRejected && callErr.Status != CallStatusFailed {
			switch {
			case runCtx.Err() != nil:
				callErr.Code, callErr.Status = ErrorCodeRunCancelled, CallStatusUnknown
			case errors.Is(callCtx.Err(), context.DeadlineExceeded):
				callErr.Code, callErr.Status = ErrorCodeCallTimeout, CallStatusUnknown
				callErr.Message = fmt.Sprintf("no response within call_timeout %s; the request may have reached the API: %s", r.limits.CallTimeout, callErr.Message)
			}
		}
		cancel()
		finish(data, callErr)
	}()
}

func (r *localRun) waitForWorkerExit(frames <-chan frameResult, grace time.Duration) {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	for {
		select {
		case frame := <-frames:
			if frame.err != nil {
				return
			}
		case <-deadline.C:
			return
		}
	}
}

// drainCalls waits for in-flight calls to observe cancellation and records
// their final journal entries. Calls that still have not returned are
// recorded as unknown: the request may have reached the API.
func (r *localRun) drainCalls(calls *sync.WaitGroup, completions <-chan completion) {
	finished := make(chan struct{})
	go func() {
		calls.Wait()
		close(finished)
	}()
	deadline := time.NewTimer(callDrainTimeout)
	defer deadline.Stop()
	for {
		select {
		case done := <-completions:
			r.journal[done.index] = done.entry
		case <-finished:
			for {
				select {
				case done := <-completions:
					r.journal[done.index] = done.entry
				default:
					r.markUnfinished()
					return
				}
			}
		case <-deadline.C:
			r.markUnfinished()
			return
		}
	}
}

func (r *localRun) markUnfinished() {
	for index := range r.journal {
		if r.journal[index].Status == callStatusActive {
			r.journal[index].Status = CallStatusUnknown
			r.journal[index].ErrorCode = ErrorCodeRunCancelled
			r.journal[index].Error = "the run ended while this call was in flight; the request may have reached the API"
		}
	}
}

// responseRequestID returns ResponseMetadata.RequestId from an OpenAPI
// response so successful calls can be audited too.
func responseRequestID(data map[string]interface{}) string {
	metadata, _ := data["ResponseMetadata"].(map[string]interface{})
	requestID, _ := metadata["RequestId"].(string)
	return requestID
}

func applyCallError(entry *JournalEntry, callErr *CallError) {
	entry.Status = callErr.Status
	if entry.Status == "" {
		entry.Status = CallStatusUnknown
	}
	entry.ErrorCode = callErr.Code
	entry.Error = callErr.Message
	entry.RequestID = callErr.RequestID
}

// resultFrame encodes a call result. A successful response that does not fit
// in one frame is replaced by a ResponseTooLarge error; replaced reports it.
func resultFrame(id int64, seq int, data map[string]interface{}, callErr *CallError, limit int) (frame []byte, replaced bool) {
	message := resultMessage{Type: "result", ID: id, Seq: seq, OK: callErr == nil, Data: data, Error: callErr}
	if message.OK && message.Data == nil {
		message.Data = map[string]interface{}{}
	}
	frame, err := encodeFrame(message)
	if err == nil && len(frame) <= limit {
		return frame, false
	}
	code, text := "ResponseTooLarge", fmt.Sprintf("the response exceeds max_frame_bytes %d", limit)
	if err != nil {
		code, text = "ResponseEncodingError", err.Error()
	}
	status := CallStatusOK
	if callErr != nil {
		status = callErr.Status
	}
	frame, _ = encodeFrame(resultMessage{Type: "result", ID: id, Seq: seq, Error: &CallError{Code: code, Status: status, Message: text}})
	return frame, true
}

func encodeFrame(value interface{}) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)))
	return append(frame, body...), nil
}

func summarize(entries []JournalEntry) Summary {
	summary := Summary{Total: len(entries)}
	for _, entry := range entries {
		switch entry.Status {
		case CallStatusOK:
			summary.OK++
		case CallStatusFailed:
			summary.Failed++
		case CallStatusRejected:
			summary.Rejected++
		default:
			summary.Unknown++
		}
	}
	return summary
}

func workerEnvironment(workDir string) []string {
	names := []string{"PATH", "LANG", "LC_ALL", "TZ"}
	if runtime.GOOS == "windows" {
		names = []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "SYSTEMDRIVE"}
	}
	environment := make([]string, 0, len(names)+3)
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	if runtime.GOOS == "windows" {
		return append(environment, "TEMP="+workDir, "TMP="+workDir)
	}
	return append(environment, "TMPDIR="+workDir)
}

// limitedBuffer keeps the first limit bytes of worker output and keeps
// draining afterwards so the worker never blocks on a full pipe.
type limitedBuffer struct {
	mu        sync.Mutex
	limit     int
	data      []byte
	truncated bool
}

func (b *limitedBuffer) drain(reader io.Reader) {
	chunk := make([]byte, 32*1024)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			b.mu.Lock()
			room := b.limit - len(b.data)
			if room > 0 {
				if n > room {
					b.data = append(b.data, chunk[:room]...)
					b.truncated = true
				} else {
					b.data = append(b.data, chunk[:n]...)
				}
			} else {
				b.truncated = true
			}
			b.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (b *limitedBuffer) text() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.data), ""), b.truncated
}
