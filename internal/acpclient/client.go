package acpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const (
	defaultClientName    = "acpdump"
	defaultClientVersion = "dev"
	closeTimeout         = 200 * time.Millisecond
)

// Config describes how to start and configure the ACP client.
type Config struct {
	Command       []string
	WorkingDir    string
	ClientName    string
	ClientVersion string
	Stderr        io.Writer
}

// Client manages an ACP subprocess and communicates via stdio using acp-go-sdk.
type Client struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	conn          *acp.ClientSideConnection
	clientName    string
	clientVersion string

	closeOnce sync.Once
	closeErr  error
}

// New starts an ACP subprocess and establishes an ACP connection.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("acp command is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	clientName := strings.TrimSpace(cfg.ClientName)
	if clientName == "" {
		clientName = defaultClientName
	}
	clientVersion := strings.TrimSpace(cfg.ClientVersion)
	if clientVersion == "" {
		clientVersion = defaultClientVersion
	}

	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.WorkingDir
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("acp stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start acp process: %w", err)
	}

	conn := acp.NewClientSideConnection(noopClient{}, stdin, stdout)
	// Suppress SDK's INFO-level "connection closed" disconnect noise
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	return &Client{
		cmd:           cmd,
		stdin:         stdin,
		conn:          conn,
		clientName:    clientName,
		clientVersion: clientVersion,
	}, nil
}

// Initialize performs ACP initialization.
func (c *Client) Initialize(ctx context.Context) (acp.InitializeResponse, error) {
	return c.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo: &acp.Implementation{
			Name:    c.clientName,
			Version: c.clientVersion,
		},
	})
}

// NewSession creates a new session in the specified working directory.
func (c *Client) NewSession(ctx context.Context, cwd string) (acp.NewSessionResponse, error) {
	return c.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
}

// Close gracefully closes the subprocess and connection.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}

		done := make(chan struct{})
		go func() {
			if c.cmd != nil {
				_ = c.cmd.Wait()
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(closeTimeout):
			if c.cmd != nil && c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			<-done
		}
	})
	return c.closeErr
}

type noopClient struct{}

var _ acp.Client = noopClient{}

func (noopClient) ReadTextFile(_ context.Context, _ acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsReadTextFile)
}

func (noopClient) WriteTextFile(_ context.Context, _ acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsWriteTextFile)
}

func (noopClient) RequestPermission(_ context.Context, _ acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}

func (noopClient) SessionUpdate(_ context.Context, _ acp.SessionNotification) error {
	return nil
}

func (noopClient) CreateTerminal(_ context.Context, _ acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalCreate)
}

func (noopClient) KillTerminal(_ context.Context, _ acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalKill)
}

func (noopClient) TerminalOutput(_ context.Context, _ acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalOutput)
}

func (noopClient) ReleaseTerminal(_ context.Context, _ acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalRelease)
}

func (noopClient) WaitForTerminalExit(_ context.Context, _ acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalWaitForExit)
}
