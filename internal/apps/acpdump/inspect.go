package acpdump

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/baldaworks/acpdump/internal/acpclient"
	"github.com/baldaworks/acpdump/internal/apps/appio"
	"github.com/baldaworks/acpdump/internal/logging"
	acp "github.com/coder/acp-go-sdk"
)

const unknownValue = "unknown"

// RunConfig describes how ACP inspection should run.
type RunConfig struct {
	Command      []string
	WorkingDir   string
	SessionModel string
	StartMessage string
	JSONOutput   bool
	Stdout       io.Writer
	Stderr       io.Writer
}

type inspectOutput struct {
	Command    []string                `json:"command"`
	Initialize *acp.InitializeResponse `json:"initialize,omitempty"`
	Session    *acp.NewSessionResponse `json:"session,omitempty"`
}

// Run connects to an ACP server command and prints initialize/session details.
func Run(ctx context.Context, cfg RunConfig) (runErr error) {
	if len(cfg.Command) == 0 {
		return fmt.Errorf("acp server command is required")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}

	startMessage := strings.TrimSpace(cfg.StartMessage)
	if startMessage == "" {
		startMessage = "inspecting ACP agent"
	}

	lockedStderr := appio.NewSyncWriter(cfg.Stderr)
	logger := logging.Ctx(ctx)
	if logging.DebugEnabled() {
		stderrLogger := logger.Output(lockedStderr)
		logger = &stderrLogger
	}

	logger.Debug().
		Str("working_dir", cfg.WorkingDir).
		Strs("command", cfg.Command).
		Msg(startMessage)

	client, err := acpclient.New(ctx, acpclient.Config{
		Command:    cfg.Command,
		WorkingDir: cfg.WorkingDir,
		Stderr:     lockedStderr,
		Debug:      logging.DebugEnabled(),
	})
	if err != nil {
		return fmt.Errorf("create acp client: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			if runErr != nil {
				logger.Debug().Err(closeErr).Msg("close ACP client after prior run error")
				return
			}
			logger.Warn().Err(closeErr).Msg("failed to close ACP client")
		}
	}()

	initResp, err := client.Initialize(ctx)
	if err != nil {
		runErr = fmt.Errorf("initialize acp client: %w", err)
		return runErr
	}
	sessionResp, err := client.NewSession(ctx, cfg.WorkingDir)
	if err != nil {
		runErr = fmt.Errorf("create acp session: %w", err)
		return runErr
	}

	output := &inspectOutput{
		Command:    append([]string(nil), cfg.Command...),
		Initialize: &initResp,
		Session:    &sessionResp,
	}
	if cfg.JSONOutput {
		runErr = writeInspectJSON(cfg.Stdout, output)
		return runErr
	}
	runErr = writeInspectHuman(cfg.Stdout, output)
	return runErr
}

func writeInspectJSON(stdout io.Writer, output *inspectOutput) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func writeInspectHuman(stdout io.Writer, output *inspectOutput) error {
	if output.Initialize == nil {
		_, err := fmt.Fprintln(stdout, "Connected to ACP agent, but initialize result is empty.")
		return err
	}

	agentName := unknownValue
	agentVersion := unknownValue
	agentTitle := ""
	if output.Initialize.AgentInfo != nil {
		if strings.TrimSpace(output.Initialize.AgentInfo.Name) != "" {
			agentName = strings.TrimSpace(output.Initialize.AgentInfo.Name)
		}
		if strings.TrimSpace(output.Initialize.AgentInfo.Version) != "" {
			agentVersion = strings.TrimSpace(output.Initialize.AgentInfo.Version)
		}
		if output.Initialize.AgentInfo.Title != nil {
			agentTitle = strings.TrimSpace(*output.Initialize.AgentInfo.Title)
		}
	}
	if _, err := fmt.Fprintf(stdout, "Agent: %s %s\n", agentName, agentVersion); err != nil {
		return err
	}
	if agentTitle != "" {
		if _, err := fmt.Fprintf(stdout, "Title: %s\n", agentTitle); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "Protocol: %d\n", output.Initialize.ProtocolVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Capabilities: %s\n", formatAgentCapabilities(output.Initialize.AgentCapabilities)); err != nil {
		return err
	}
	if err := writeAuthMethodSummary(stdout, output.Initialize.AuthMethods); err != nil {
		return err
	}
	return writeSessionSummary(stdout, output.Session)
}

func formatAgentCapabilities(caps acp.AgentCapabilities) string {
	return fmt.Sprintf(
		"load_session=%t, mcp(http=%t,sse=%t), prompt(audio=%t,image=%t,embedded_context=%t)",
		caps.LoadSession,
		caps.McpCapabilities.Http,
		caps.McpCapabilities.Sse,
		caps.PromptCapabilities.Audio,
		caps.PromptCapabilities.Image,
		caps.PromptCapabilities.EmbeddedContext,
	)
}

func writeAuthMethodSummary(stdout io.Writer, methods []acp.AuthMethod) error {
	if _, err := fmt.Fprintf(stdout, "Auth methods (%d):\n", len(methods)); err != nil {
		return err
	}
	for _, method := range methods {
		id, name, description := authMethodSummary(method)
		line := fmt.Sprintf("- %s: %s", id, name)
		if description != "" {
			line += ": " + description
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func authMethodSummary(method acp.AuthMethod) (string, string, string) {
	switch {
	case method.EnvVar != nil:
		return method.EnvVar.Id, strings.TrimSpace(method.EnvVar.Name), trimOptionalString(method.EnvVar.Description)
	case method.Terminal != nil:
		return method.Terminal.Id, strings.TrimSpace(method.Terminal.Name), trimOptionalString(method.Terminal.Description)
	case method.Agent != nil:
		return method.Agent.Id, strings.TrimSpace(method.Agent.Name), trimOptionalString(method.Agent.Description)
	default:
		return unknownValue, unknownValue, ""
	}
}

func trimOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func writeSessionSummary(stdout io.Writer, session *acp.NewSessionResponse) error {
	if session == nil {
		_, err := fmt.Fprintln(stdout, "Session: unavailable")
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Session: %s\n", session.SessionId); err != nil {
		return err
	}
	if len(session.ConfigOptions) > 0 {
		return writeSessionConfigSummary(stdout, session.ConfigOptions)
	}
	return writeSessionModeSummary(stdout, session.Modes)
}

func writeSessionConfigSummary(stdout io.Writer, options []acp.SessionConfigOption) error {
	if _, err := fmt.Fprintf(stdout, "Session config options (%d):\n", len(options)); err != nil {
		return err
	}
	for _, option := range options {
		switch {
		case option.Select != nil:
			if err := writeSessionConfigSelect(stdout, option.Select); err != nil {
				return err
			}
		case option.Boolean != nil:
			value := option.Boolean
			line := fmt.Sprintf("%s, current=%t", sessionConfigLabel(value.Id, value.Name, value.Category, "boolean"), value.CurrentValue)
			if _, err := fmt.Fprintln(stdout, appendDescription(line, value.Description)); err != nil {
				return err
			}
		}
	}
	return nil
}

func sessionConfigLabel(id acp.SessionConfigId, name string, category *acp.SessionConfigOptionCategory, kind string) string {
	metadata := "type=" + kind
	if category != nil && *category != "" {
		metadata = "category=" + string(*category) + ", " + metadata
	}
	return fmt.Sprintf("- %s: %s (%s)", id, strings.TrimSpace(name), metadata)
}

func appendDescription(line string, description *string) string {
	if text := trimOptionalString(description); text != "" {
		return line + ": " + text
	}
	return line
}

func writeSessionConfigSelect(stdout io.Writer, option *acp.SessionConfigOptionSelect) error {
	count := 0
	if option.Options.Ungrouped != nil {
		count = len(*option.Options.Ungrouped)
	} else if option.Options.Grouped != nil {
		for _, group := range *option.Options.Grouped {
			count += len(group.Options)
		}
	}
	line := fmt.Sprintf("%s, current=%s, available (%d):", sessionConfigLabel(option.Id, option.Name, option.Category, "select"), option.CurrentValue, count)
	if description := trimOptionalString(option.Description); description != "" {
		line += " " + description
	}
	if _, err := fmt.Fprintln(stdout, line); err != nil {
		return err
	}
	if option.Options.Ungrouped != nil {
		return writeSessionConfigChoices(stdout, *option.Options.Ungrouped, "  ")
	}
	if option.Options.Grouped != nil {
		for _, group := range *option.Options.Grouped {
			if _, err := fmt.Fprintf(stdout, "  %s: %s\n", group.Group, strings.TrimSpace(group.Name)); err != nil {
				return err
			}
			if err := writeSessionConfigChoices(stdout, group.Options, "    "); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeSessionConfigChoices(stdout io.Writer, choices []acp.SessionConfigSelectOption, indent string) error {
	for _, choice := range choices {
		line := fmt.Sprintf("%s- %s: %s", indent, choice.Value, strings.TrimSpace(choice.Name))
		if _, err := fmt.Fprintln(stdout, appendDescription(line, choice.Description)); err != nil {
			return err
		}
	}
	return nil
}

func writeSessionModeSummary(stdout io.Writer, modes *acp.SessionModeState) error {
	if modes == nil {
		_, err := fmt.Fprintln(stdout, "Session modes: unavailable")
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Session modes: current=%s, available (%d):\n", modes.CurrentModeId, len(modes.AvailableModes)); err != nil {
		return err
	}
	for _, mode := range modes.AvailableModes {
		line := fmt.Sprintf("- %s: %s", mode.Id, strings.TrimSpace(mode.Name))
		if mode.Description != nil && strings.TrimSpace(*mode.Description) != "" {
			line += ": " + strings.TrimSpace(*mode.Description)
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return err
		}
	}
	return nil
}
