package goproxy

import (
	"io"
	"net"
	"net/http"
	"strings"
)

// WebSocketDirection indicates the direction of WebSocket data flow.
type WebSocketDirection int

const (
	// WebSocketClientToServer indicates data flowing from client to remote server.
	WebSocketClientToServer WebSocketDirection = iota
	// WebSocketServerToClient indicates data flowing from remote server to client.
	WebSocketServerToClient
)

// WebSocketHandler provides an interface for intercepting WebSocket traffic.
// Implementations can inspect, modify, or log the raw WebSocket data stream
// flowing in either direction.
type WebSocketHandler interface {
	// HandleWebSocket is called to handle bidirectional WebSocket data transfer.
	// The handler receives the remote server connection, the proxy client connection,
	// and the proxy context. The handler is responsible for copying data between
	// the connections (typically in both directions using goroutines).
	// The function should block until the WebSocket connection is closed.
	HandleWebSocket(remoteConn io.ReadWriter, proxyClient io.ReadWriter, ctx *ProxyCtx)
}

// FuncWebSocketHandler is a wrapper that converts a function to a WebSocketHandler interface type.
type FuncWebSocketHandler func(remoteConn io.ReadWriter, proxyClient io.ReadWriter, ctx *ProxyCtx)

// HandleWebSocket implements the WebSocketHandler interface.
func (f FuncWebSocketHandler) HandleWebSocket(remoteConn io.ReadWriter, proxyClient io.ReadWriter, ctx *ProxyCtx) {
	f(remoteConn, proxyClient, ctx)
}

// WebSocketCopyHandler provides a simpler interface for intercepting WebSocket data
// by replacing the io.Copy function used for data transfer. This allows inspection
// or modification of data without implementing the full bidirectional flow.
type WebSocketCopyHandler func(dst io.Writer, src io.Reader, direction WebSocketDirection, ctx *ProxyCtx) (int64, error)

// WebSocketCloseHandler is called when the WebSocket proxy connection is fully closed.
// This allows cleanup of resources associated with the WebSocket connection.
type WebSocketCloseHandler func(ctx *ProxyCtx)

func headerContains(header http.Header, name string, value string) bool {
	for _, v := range header[name] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(value, strings.TrimSpace(s)) {
				return true
			}
		}
	}
	return false
}

func isWebSocketHandshake(header http.Header) bool {
	return headerContains(header, "Connection", "Upgrade") &&
		headerContains(header, "Upgrade", "websocket")
}

func (proxy *ProxyHttpServer) hijackConnection(ctx *ProxyCtx, w http.ResponseWriter) (net.Conn, error) {
	// Connect to Client
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("httpserver does not support hijacking")
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		ctx.Warnf("Hijack error: %v", err)
		return nil, err
	}
	return clientConn, nil
}

func (proxy *ProxyHttpServer) proxyWebsocket(ctx *ProxyCtx, remoteConn io.ReadWriter, proxyClient io.ReadWriter) {
	// If a full WebSocket handler is set, delegate to it entirely
	if ctx.WebSocketHandler != nil {
		ctx.WebSocketHandler.HandleWebSocket(remoteConn, proxyClient, ctx)
		return
	}

	// Ensure cleanup handler is called when done
	defer func() {
		if ctx.WebSocketCloseHandler != nil {
			ctx.WebSocketCloseHandler(ctx)
		}
	}()

	// 2 is the number of goroutines, this code is implemented according to
	// https://stackoverflow.com/questions/52031332/wait-for-one-goroutine-to-finish
	waitChan := make(chan struct{}, 2)

	// Use custom copy handler if set, otherwise use default copyOrWarn
	copyFunc := func(dst io.Writer, src io.Reader, direction WebSocketDirection) error {
		if ctx.WebSocketCopyHandler != nil {
			_, err := ctx.WebSocketCopyHandler(dst, src, direction, ctx)
			return err
		}
		return copyOrWarn(ctx, dst, src)
	}

	go func() {
		_ = copyFunc(remoteConn, proxyClient, WebSocketClientToServer)
		waitChan <- struct{}{}
	}()

	go func() {
		_ = copyFunc(proxyClient, remoteConn, WebSocketServerToClient)
		waitChan <- struct{}{}
	}()

	// Wait for BOTH directions to complete to avoid goroutine leaks
	<-waitChan
	<-waitChan
}
