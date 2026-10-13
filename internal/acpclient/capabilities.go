package acpclient

import (
	"encoding/json"
	"io"

	acp "github.com/coder/acp-go-sdk"
)

// booleanConfigWriter fills the capability missing from SDK v0.13.5 bindings.
// The SDK sends each complete JSON-RPC message in a single Write call.
type booleanConfigWriter struct {
	io.Writer
}

func (w booleanConfigWriter) Write(p []byte) (int, error) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(p, &message); err != nil {
		return 0, err
	}
	var method string
	if err := json.Unmarshal(message["method"], &method); err != nil || method != acp.AgentMethodInitialize {
		return w.Writer.Write(p)
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(message["params"], &params); err != nil {
		return 0, err
	}
	var capabilities map[string]json.RawMessage
	if raw := params["clientCapabilities"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &capabilities); err != nil {
			return 0, err
		}
	}
	if capabilities == nil {
		capabilities = make(map[string]json.RawMessage)
	}
	capabilities["session"] = json.RawMessage(`{"configOptions":{"boolean":{}}}`)
	var err error
	params["clientCapabilities"], err = json.Marshal(capabilities)
	if err != nil {
		return 0, err
	}
	message["params"], err = json.Marshal(params)
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}
	payload = append(payload, '\n')
	n, err := w.Writer.Write(payload)
	if err != nil {
		return 0, err
	}
	if n != len(payload) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
