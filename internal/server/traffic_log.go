package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type trafficLog struct {
	mu sync.Mutex
	w  io.Writer
}

const maxHTTPDebugBodyBytes = 1 << 20

type trafficLogEntry struct {
	Time          string          `json:"time"`
	Transport     string          `json:"transport"`
	Direction     string          `json:"direction"`
	Method        string          `json:"method,omitempty"`
	Path          string          `json:"path,omitempty"`
	Status        int             `json:"status,omitempty"`
	Message       json.RawMessage `json:"message,omitempty"`
	Body          string          `json:"body,omitempty"`
	BodyTruncated bool            `json:"body_truncated,omitempty"`
	Error         string          `json:"error,omitempty"`
}

func openTrafficLog(path string) (*trafficLog, io.Closer, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("debug traffic log path must not be a symlink")
		}
		if !info.Mode().IsRegular() {
			return nil, nil, errors.New("debug traffic log path must be a regular file")
		}
		if !trafficLogFilePermissionsSafe(info.Mode()) {
			return nil, nil, fmt.Errorf("debug traffic log file permissions must not allow group or other access: %s", info.Mode().Perm())
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	file, err := openTrafficLogFile(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, errors.New("debug traffic log path must be a regular file")
	}
	if !trafficLogFilePermissionsSafe(info.Mode()) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("debug traffic log file permissions must not allow group or other access: %s", info.Mode().Perm())
	}
	return &trafficLog{w: file}, file, nil
}

func (l *trafficLog) write(entry trafficLogEntry) {
	if l == nil || l.w == nil {
		return
	}
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(entry)
	if err != nil {
		payload = []byte(`{"direction":"log_error","body":"failed to marshal traffic log entry"}`)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(payload, '\n'))
}

type loggingTransport struct {
	transport mcp.Transport
	log       *trafficLog
	name      string
}

func (t loggingTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return loggingConnection{Connection: conn, log: t.log, transport: t.name}, nil
}

type loggingConnection struct {
	mcp.Connection
	log       *trafficLog
	transport string
}

func (c loggingConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err == nil {
		c.logMessage("inbound", msg)
	}
	return msg, err
}

func (c loggingConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.logMessage("outbound", msg)
	return c.Connection.Write(ctx, msg)
}

func (c loggingConnection) logMessage(direction string, msg jsonrpc.Message) {
	payload, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		c.log.write(trafficLogEntry{Transport: c.transport, Direction: direction, Body: "failed to encode JSON-RPC message"})
		return
	}
	c.log.write(trafficLogEntry{Transport: c.transport, Direction: direction, Message: payload})
}

func (l *trafficLog) middleware(transport string, next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body []byte
		var truncated bool
		if req.Body != nil {
			var err error
			body, truncated, err = readHTTPDebugBody(req.Body)
			_ = req.Body.Close()
			if err != nil {
				l.write(trafficLogEntry{
					Transport: transport,
					Direction: "inbound",
					Method:    req.Method,
					Path:      req.URL.RequestURI(),
					Error:     err.Error(),
				})
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				l.write(trafficLogEntry{
					Transport: transport,
					Direction: "outbound",
					Method:    req.Method,
					Path:      req.URL.RequestURI(),
					Status:    http.StatusBadRequest,
					Body:      "failed to read request body\n",
				})
				return
			}
			if truncated {
				l.write(trafficLogEntry{
					Transport:     transport,
					Direction:     "inbound",
					Method:        req.Method,
					Path:          req.URL.RequestURI(),
					Body:          string(body),
					BodyTruncated: true,
				})
				http.Error(w, "debug traffic log request body limit exceeded", http.StatusRequestEntityTooLarge)
				l.write(trafficLogEntry{
					Transport: transport,
					Direction: "outbound",
					Method:    req.Method,
					Path:      req.URL.RequestURI(),
					Status:    http.StatusRequestEntityTooLarge,
					Body:      "debug traffic log request body limit exceeded\n",
				})
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		l.write(trafficLogEntry{
			Transport:     transport,
			Direction:     "inbound",
			Method:        req.Method,
			Path:          req.URL.RequestURI(),
			Body:          string(body),
			BodyTruncated: truncated,
		})

		recorder := &trafficResponseRecorder{
			ResponseWriter: w,
			log:            l,
			transport:      transport,
			method:         req.Method,
			path:           req.URL.RequestURI(),
		}
		next.ServeHTTP(recorder, req)
		l.write(trafficLogEntry{
			Transport:     transport,
			Direction:     "outbound",
			Method:        req.Method,
			Path:          req.URL.RequestURI(),
			Status:        recorder.status(),
			Body:          recorder.body.String(),
			BodyTruncated: recorder.bodyTruncated,
		})
	})
}

type trafficResponseRecorder struct {
	http.ResponseWriter
	body          bytes.Buffer
	bodyTruncated bool
	statusCode    int
	log           *trafficLog
	transport     string
	method        string
	path          string
}

func (r *trafficResponseRecorder) WriteHeader(statusCode int) {
	if r.statusCode != 0 {
		return
	}
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *trafficResponseRecorder) Write(data []byte) (int, error) {
	if r.isEventStream() {
		r.log.write(trafficLogEntry{
			Transport:     r.transport,
			Direction:     "outbound_chunk",
			Method:        r.method,
			Path:          r.path,
			Status:        r.status(),
			Body:          string(truncateBytes(data, maxHTTPDebugBodyBytes)),
			BodyTruncated: len(data) > maxHTTPDebugBodyBytes,
		})
		return r.ResponseWriter.Write(data)
	}
	if remaining := maxHTTPDebugBodyBytes - r.body.Len(); remaining > 0 {
		chunk := data
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
			r.bodyTruncated = true
		}
		_, _ = r.body.Write(chunk)
	} else if len(data) > 0 {
		r.bodyTruncated = true
	}
	return r.ResponseWriter.Write(data)
}

func (r *trafficResponseRecorder) status() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

func (r *trafficResponseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *trafficResponseRecorder) isEventStream() bool {
	return strings.HasPrefix(strings.ToLower(r.Header().Get("Content-Type")), "text/event-stream")
}

func readHTTPDebugBody(body io.Reader) ([]byte, bool, error) {
	payload, err := io.ReadAll(io.LimitReader(body, maxHTTPDebugBodyBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(payload) > maxHTTPDebugBodyBytes {
		return payload[:maxHTTPDebugBodyBytes], true, nil
	}
	return payload, false, nil
}

func truncateBytes(data []byte, limit int) []byte {
	if len(data) <= limit {
		return data
	}
	return data[:limit]
}
