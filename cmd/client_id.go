package cmd

import (
	"fmt"

	"github.com/volcengine/volcengine-cli/internal/clientid"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

// applyClientID runs after request construction and before signing. It shares
// the documentation commands' persistent identity and never sends a request
// with an unreadable, invalid, or unpersisted client ID.
func applyClientID(req *request.Request) {
	if req.Error != nil {
		return
	}
	directory, err := configFileDirFunc()
	if err != nil {
		req.Error = fmt.Errorf("load client ID: %w", err)
		return
	}
	id, err := clientid.LoadOrCreate(directory)
	if err != nil {
		req.Error = fmt.Errorf("load client ID: %w", err)
		return
	}
	// The locally persisted ID is authoritative, including when --header was
	// used to supply a different X-Client-Id value.
	req.HTTPRequest.Header.Set(clientid.HeaderName, id)
}
