package goproxy

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestHeaderContains(t *testing.T) {
	header := http.Header{}
	header.Add("Connection", "Upgrade, keep-alive")
	header.Add("Upgrade", "websocket")

	if !headerContains(header, "Connection", "Upgrade") {
		t.Error("Expected headerContains to return true for Connection: Upgrade")
	}
	if !headerContains(header, "Connection", "keep-alive") {
		t.Error("Expected headerContains to return true for Connection: keep-alive")
	}
	if !headerContains(header, "Upgrade", "websocket") {
		t.Error("Expected headerContains to return true for Upgrade: websocket")
	}
	if headerContains(header, "Connection", "close") {
		t.Error("Expected headerContains to return false for Connection: close")
	}
}

func TestIsWebSocketHandshake(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected bool
	}{
		{
			name: "valid websocket handshake",
			headers: http.Header{
				"Connection": []string{"Upgrade"},
				"Upgrade":    []string{"websocket"},
			},
			expected: true,
		},
		{
			name: "valid websocket handshake with multiple connection values",
			headers: http.Header{
				"Connection": []string{"Upgrade, keep-alive"},
				"Upgrade":    []string{"websocket"},
			},
			expected: true,
		},
		{
			name: "missing upgrade header",
			headers: http.Header{
				"Connection": []string{"Upgrade"},
			},
			expected: false,
		},
		{
			name: "missing connection header",
			headers: http.Header{
				"Upgrade": []string{"websocket"},
			},
			expected: false,
		},
		{
			name:     "empty headers",
			headers:  http.Header{},
			expected: false,
		},
		{
			name: "wrong upgrade value",
			headers: http.Header{
				"Connection": []string{"Upgrade"},
				"Upgrade":    []string{"h2c"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWebSocketHandshake(tt.headers)
			if result != tt.expected {
				t.Errorf("isWebSocketHandshake() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFuncWebSocketHandler(t *testing.T) {
	called := false
	var capturedCtx *ProxyCtx

	handler := FuncWebSocketHandler(func(remoteConn io.ReadWriter, proxyClient io.ReadWriter, ctx *ProxyCtx) {
		called = true
		capturedCtx = ctx
	})

	ctx := &ProxyCtx{Session: 42}
	remote := &bytes.Buffer{}
	client := &bytes.Buffer{}

	handler.HandleWebSocket(remote, client, ctx)

	if !called {
		t.Error("Expected handler to be called")
	}
	if capturedCtx != ctx {
		t.Error("Expected context to be passed to handler")
	}
}

func TestWebSocketCopyHandler(t *testing.T) {
	var directions []WebSocketDirection
	var dataCopied []string

	copyHandler := WebSocketCopyHandler(func(dst io.Writer, src io.Reader, direction WebSocketDirection, ctx *ProxyCtx) (int64, error) {
		directions = append(directions, direction)
		data, err := io.ReadAll(src)
		if err != nil {
			return 0, err
		}
		dataCopied = append(dataCopied, string(data))
		n, err := dst.Write(data)
		return int64(n), err
	})

	src := bytes.NewBufferString("test data")
	dst := &bytes.Buffer{}
	ctx := &ProxyCtx{}

	n, err := copyHandler(dst, src, WebSocketClientToServer, ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if n != 9 {
		t.Errorf("Expected 9 bytes copied, got %d", n)
	}
	if dst.String() != "test data" {
		t.Errorf("Expected 'test data', got '%s'", dst.String())
	}
	if len(directions) != 1 || directions[0] != WebSocketClientToServer {
		t.Error("Expected direction to be captured")
	}
}

func TestWebSocketDirectionConstants(t *testing.T) {
	// Ensure the constants have distinct values
	if WebSocketClientToServer == WebSocketServerToClient {
		t.Error("WebSocket direction constants should have different values")
	}
}
