package scriptrun

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the worker/broker frame protocol version. Frames are a
// 4-byte big-endian length followed by one UTF-8 JSON object. The same
// contract is used by the local CPython worker and must be honored by any
// hosted sandbox adapter that proxies calls back to a broker.
const ProtocolVersion = 1

var errFrameTooLarge = errors.New("protocol frame exceeds the size limit")

// workerMessage is a frame sent by the worker to the broker.
type workerMessage struct {
	Type       string                 `json:"type"`
	ID         *int64                 `json:"id,omitempty"`
	Service    string                 `json:"service,omitempty"`
	API        string                 `json:"api,omitempty"`
	Region     string                 `json:"region,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Result     json.RawMessage        `json:"result,omitempty"`
	Error      *ScriptError           `json:"error,omitempty"`
}

// startMessage is the first broker frame. User code runs only after it.
type startMessage struct {
	Type     string          `json:"type"`
	Protocol int             `json:"protocol"`
	Code     string          `json:"code"`
	Args     json.RawMessage `json:"args"`
}

type resultMessage struct {
	Type  string                 `json:"type"`
	ID    int64                  `json:"id"`
	Seq   int                    `json:"seq"`
	OK    bool                   `json:"ok"`
	Data  map[string]interface{} `json:"data,omitempty"`
	Error *CallError             `json:"error,omitempty"`
}

type cancelMessage struct {
	Type string `json:"type"`
}

// readFrame reads one frame and rejects bodies larger than limit before
// allocating them.
func readFrame(reader io.Reader, limit int) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if int64(length) > int64(limit) {
		return nil, errFrameTooLarge
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return body, nil
}

func decodeWorkerMessage(body []byte) (*workerMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var message workerMessage
	if err := decoder.Decode(&message); err != nil {
		return nil, fmt.Errorf("invalid worker frame: %w", err)
	}
	return &message, nil
}
