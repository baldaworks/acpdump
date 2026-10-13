package acpclient

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestBooleanConfigWriterPassesOtherMessagesUnchanged(t *testing.T) {
	for _, wire := range []string{
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"session/new\",\"params\":{\"cwd\":\"/tmp\",\"mcpServers\":[]}}\n",
		"{\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{}}\n",
	} {
		var output bytes.Buffer
		writer := booleanConfigWriter{Writer: &output}
		n, err := writer.Write([]byte(wire))
		if err != nil || n != len(wire) || output.String() != wire {
			t.Fatalf("passthrough write = (%d, %v), output = %q, want %q", n, err, output.String(), wire)
		}
	}
}

func TestBooleanConfigWriterReportsOriginalLength(t *testing.T) {
	wire := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n")
	var output bytes.Buffer
	n, err := (booleanConfigWriter{Writer: &output}).Write(wire)
	if err != nil || n != len(wire) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(wire))
	}
	if output.Len() <= len(wire) {
		t.Fatal("initialize capability was not added")
	}
}

func TestBooleanConfigWriterPropagatesWriteFailure(t *testing.T) {
	wire := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	for _, tt := range []struct {
		name string
		err  error
		want error
	}{
		{name: "write error", err: io.ErrClosedPipe, want: io.ErrClosedPipe},
		{name: "short write", want: io.ErrShortWrite},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writer := booleanConfigWriter{Writer: configWriteFailure{err: tt.err}}
			if _, err := writer.Write(wire); !errors.Is(err, tt.want) {
				t.Fatalf("Write() error = %v, want %v", err, tt.want)
			}
		})
	}
}

type configWriteFailure struct {
	err error
}

func (w configWriteFailure) Write(_ []byte) (int, error) {
	return 0, w.err
}
