package acpdump

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/acpdump/internal/logging"
	acp "github.com/coder/acp-go-sdk"
	"github.com/rs/zerolog/log"
)

func TestRunDiscoversConfigurationOptions(t *testing.T) {
	t.Setenv("GO_WANT_ACPDUMP_HELPER", "1")
	t.Setenv("GO_ACPDUMP_HELPER_CONFIG", `[{"id":"model","name":"Model","category":"model","type":"select","currentValue":"m1","options":[{"value":"m1","name":"One"}]},{"id":"toggle","name":"Toggle","type":"boolean","currentValue":true}]`)
	if err := logging.Init(); err != nil {
		t.Fatal(err)
	}
	for _, asJSON := range []bool{false, true} {
		var stdout bytes.Buffer
		stderr := &lockedBuffer{}
		if err := Run(log.Logger.WithContext(context.Background()), RunConfig{
			Command: acpDumpHelperCommand(), WorkingDir: t.TempDir(),
			JSONOutput: asJSON, Stdout: &stdout, Stderr: stderr,
		}); err != nil {
			t.Fatal(err)
		}
		if asJSON {
			var output inspectOutput
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if output.Session == nil || len(output.Session.ConfigOptions) != 2 || output.Session.ConfigOptions[1].Boolean == nil || !output.Session.ConfigOptions[1].Boolean.CurrentValue {
				t.Fatalf("JSON lost advertised configuration: %s", stdout.String())
			}
		} else if !strings.Contains(stdout.String(), "current=m1, available (1)") || !strings.Contains(stdout.String(), "- toggle: Toggle (type=boolean), current=true") {
			t.Fatalf("human output lost advertised configuration: %s", stdout.String())
		}
		if stderr.String() != "" {
			t.Fatalf("unexpected stderr: %s", stderr.String())
		}
	}
}

func TestSessionConfigurationOutput(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want string
	}{
		{
			name: "flat selectors prefer config over legacy modes",
			wire: `{"sessionId":"s1","modes":{"currentModeId":"old","availableModes":[]},"configOptions":[
				{"id":"model","name":" Model ","description":" Choose a model ","category":"model","type":"select","currentValue":"m2","options":[{"value":"m1","name":" Fast ","description":" Quick replies "},{"value":"m2","name":"Smart"}]},
				{"id":"mode","name":"Mode","category":"mode","type":"select","currentValue":"plan","options":[{"value":"plan","name":"Plan"}]},
				{"id":"effort","name":"Reasoning","category":"thought_level","type":"select","currentValue":"high","options":[{"value":"high","name":"High","description":"  "}]}
			]}`,
			want: "Session: s1\nSession config options (3):\n" +
				"- model: Model (category=model, type=select), current=m2, available (2): Choose a model\n" +
				"  - m1: Fast: Quick replies\n  - m2: Smart\n" +
				"- mode: Mode (category=mode, type=select), current=plan, available (1):\n  - plan: Plan\n" +
				"- effort: Reasoning (category=thought_level, type=select), current=high, available (1):\n  - high: High\n",
		},
		{
			name: "grouped choices count values and preserve empty groups",
			wire: `{"sessionId":"s1","configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"m3","options":[
				{"group":"a","name":" Provider A ","options":[{"value":"m1","name":"One"},{"value":"m2","name":"Two"}]},
				{"group":"b","name":"Provider B","options":[{"value":"m3","name":"Three"}]},
				{"group":"empty","name":"Empty","options":[]}
			]}]}`,
			want: "Session: s1\nSession config options (1):\n" +
				"- model: Model (type=select), current=m3, available (3):\n" +
				"  a: Provider A\n    - m1: One\n    - m2: Two\n" +
				"  b: Provider B\n    - m3: Three\n  empty: Empty\n",
		},
		{
			name: "custom and missing categories and boolean values",
			wire: `{"sessionId":"s1","configOptions":[
				{"id":"speed","name":"Speed","category":"model_config","type":"select","currentValue":"fast","options":[]},
				{"id":"custom","name":"Custom","category":"_provider","type":"select","currentValue":"x","options":[]},
				{"id":"enabled","name":" Enabled ","description":" A toggle ","type":"boolean","currentValue":true},
				{"id":"disabled","name":"Disabled","category":"_custom","type":"boolean","currentValue":false}
			]}`,
			want: "Session: s1\nSession config options (4):\n" +
				"- speed: Speed (category=model_config, type=select), current=fast, available (0):\n" +
				"- custom: Custom (category=_provider, type=select), current=x, available (0):\n" +
				"- enabled: Enabled (type=boolean), current=true: A toggle\n" +
				"- disabled: Disabled (category=_custom, type=boolean), current=false\n",
		},
		{
			name: "legacy modes fallback",
			wire: `{"sessionId":"s1","modes":{"currentModeId":"plan","availableModes":[{"id":"plan","name":" Plan ","description":" Think first "}]}}`,
			want: "Session: s1\nSession modes: current=plan, available (1):\n- plan: Plan: Think first\n",
		},
		{
			name: "empty config falls back",
			wire: `{"sessionId":"s1","configOptions":[]}`,
			want: "Session: s1\nSession modes: unavailable\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var session acp.NewSessionResponse
			if err := json.Unmarshal([]byte(tt.wire), &session); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := writeSessionSummary(&out, &session); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tt.want {
				t.Fatalf("session output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestConfigurationJSONPreservesAdvertisedOptions(t *testing.T) {
	wire := `{"sessionId":"s1","configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"m1","options":[{"group":"g","name":"Group","options":[{"value":"m1","name":"One"}]}]},{"id":"toggle","name":"Toggle","type":"boolean","currentValue":false}]}`
	var session acp.NewSessionResponse
	if err := json.Unmarshal([]byte(wire), &session); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeInspectJSON(&out, &inspectOutput{Session: &session}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Session map[string]any `json:"session"`
	}
	var want map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wire), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Session, want) {
		t.Fatalf("JSON session = %#v, want %#v", got.Session, want)
	}
}

func TestConfigurationOutputPropagatesWriterFailures(t *testing.T) {
	wire := `{"sessionId":"s1","configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"m1","options":[{"group":"g","name":"Group","options":[{"value":"m1","name":"One","description":"Description"}]}]},{"id":"toggle","name":"Toggle","type":"boolean","currentValue":true}]}`
	var session acp.NewSessionResponse
	if err := json.Unmarshal([]byte(wire), &session); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeSessionSummary(&output, &session); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "toggle") {
		t.Fatal("configuration output missing")
	}
	for limit := 0; limit < output.Len(); limit++ {
		writer := &failingConfigWriter{remaining: limit}
		if err := writeSessionSummary(writer, &session); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("write failure at byte %d: got %v", limit, err)
		}
	}
}

type failingConfigWriter struct {
	remaining int
}

func (w *failingConfigWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, io.ErrClosedPipe
	}
	w.remaining -= len(p)
	return len(p), nil
}
