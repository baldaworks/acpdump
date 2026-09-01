package acpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClientLifecycle(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	var stderr bytes.Buffer
	client, err := New(context.Background(), Config{
		Command:    []string{os.Args[0], "-test.run=TestHelperProcess", "--"},
		WorkingDir: t.TempDir(),
		Stderr:     &stderr,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	initResp, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}
	if initResp.AgentInfo == nil || initResp.AgentInfo.Name != "mock-agent" {
		t.Errorf("unexpected agent info: %+v", initResp.AgentInfo)
	}

	sessionResp, err := client.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}
	if sessionResp.SessionId != "mock-session-1" {
		t.Errorf("unexpected session id: %s", sessionResp.SessionId)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify no "connection closed" or "peer connection closed" noise in stderr
	if strings.Contains(stderr.String(), "connection closed") || strings.Contains(stderr.String(), "peer connection closed") {
		t.Errorf("stderr contains connection closed noise: %q", stderr.String())
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      mustRaw(req.ID),
				"result": map[string]any{
					"protocolVersion": 1,
					"agentInfo": map[string]any{
						"name":    "mock-agent",
						"version": "1.0.0",
					},
					"agentCapabilities": map[string]any{
						"loadSession": false,
						"mcpCapabilities": map[string]any{
							"http": false,
							"sse":  false,
						},
						"promptCapabilities": map[string]any{
							"audio":           false,
							"image":           false,
							"embeddedContext": false,
						},
					},
					"authMethods": []any{},
				},
			}
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			_, _ = os.Stdout.Write(data)

		case "session/new":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      mustRaw(req.ID),
				"result": map[string]any{
					"sessionId": "mock-session-1",
				},
			}
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			_, _ = os.Stdout.Write(data)
		}
	}
	os.Exit(0)
}

func mustRaw(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	var v any
	_ = json.Unmarshal(r, &v)
	return v
}
