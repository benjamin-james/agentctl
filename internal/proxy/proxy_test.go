package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Write a shell script that acts as the named agent type
// with three dumb commands
func fakeAgent(t *testing.T, kind string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fake-agent")
	var script string
	switch kind {
	case "echo":
		script = "#!/bin/sh\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"
	case "spam":
		script = "#!/bin/sh\nwhile true; do printf 'hello\\n'; done\n"
	case "exit":
		script = "#!/bin/sh\nexit 42\n"
	default:
		t.Fatalf("unknown fake agent kind %q", kind)
	}
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// Start proxy with a fake agent, starts Run in goroutine,
// returns socket path + cancel func
func startProxy(t *testing.T, agentPath string) (socket string, cancel func()) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "agent.sock")
	cfg := Config{
		SocketPath: socket,
		AgentPath:  agentPath,
		Workdir:    t.TempDir(),
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Error("proxy did not shut down within 5s")
		}
	})
	return socket, cancel
}

// Dial retries until proxy is ready
func dialRetry(t *testing.T, socket string) net.Conn {
	t.Helper()
	var conn net.Conn
	var err error
	for i := 0; i < 200; i++ {
		conn, err = net.Dial("unix", socket)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("dial %s: %v", socket, err)
	return nil
}

// Wrap net.Conn with a persistent bufio.Reader st
// buffered bytes survive after multiple readLine calls
type connReader struct {
	*bufio.Reader
	conn net.Conn
}

func newConnReader(c net.Conn) *connReader {
	return &connReader{Reader: bufio.NewReader(c), conn: c}
}

func readLine(t *testing.T, cr *connReader, timeout time.Duration) string {
	t.Helper()
	if err := cr.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	line, err := cr.ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	return strings.TrimRight(line, "\n")
}

func writeLine(t *testing.T, conn net.Conn, s string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "%s\n", s); err != nil {
		t.Fatalf("write line: %v", err)
	}
}

func TestEchoRoundTrip(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	conn := dialRetry(t, socket)
	defer func() { _ = conn.Close() }()
	cr := newConnReader(conn)

	writeLine(t, conn, "ping")
	if got := readLine(t, cr, 3*time.Second); got != "ping" {
		t.Errorf("got %q, want %q", got, "ping")
	}

	writeLine(t, conn, "hello")
	if got := readLine(t, cr, 3*time.Second); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestEvictionClosesPreviousClient(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	c1 := dialRetry(t, socket)
	defer func() { _ = c1.Close() }()
	cr1 := newConnReader(c1)
	writeLine(t, c1, "first")
	_ = readLine(t, cr1, 3*time.Second) // confirm c1 is attached

	c2 := dialRetry(t, socket)
	defer func() { _ = c2.Close() }()
	cr2 := newConnReader(c2)

	// c1 should now be evicted: reading should return EOF or error.
	if err := c1.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c1.Read(buf); err == nil {
		t.Error("expected c1 to be closed after eviction, but Read succeeded")
	}

	// c2 should be operational.
	writeLine(t, c2, "second")
	if got := readLine(t, cr2, 3*time.Second); got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestDisconnectKeepsAgentAlive(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	c1 := dialRetry(t, socket)
	cr1 := newConnReader(c1)
	writeLine(t, c1, "first")
	_ = readLine(t, cr1, 3*time.Second)
	_ = c1.Close()

	// Give the pump time to notice the disconnect and set current=nil.
	time.Sleep(200 * time.Millisecond)

	// A new client should still be able to talk to the same agent process.
	c2 := dialRetry(t, socket)
	defer func() { _ = c2.Close() }()
	cr2 := newConnReader(c2)
	writeLine(t, c2, "second")
	if got := readLine(t, cr2, 3*time.Second); got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestBackpressureDeliversDelayedOutput(t *testing.T) {
	// agent that runs `yes "hello"`
	// Without agent the pipe should fill and agent blocks
	// but when connects the buffered/subseuquent output is delivered
	socket, _ := startProxy(t, fakeAgent(t, "spam"))

	// wait to ensure writing / blocking are flushed before connecting
	time.Sleep(300 * time.Millisecond)

	c := dialRetry(t, socket)
	defer func() { _ = c.Close() }()
	cr := newConnReader(c)

	// Ensure the hello lines were received
	got := readLine(t, cr, 3*time.Second)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestAgentExitShutsDownProxy(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	cfg := Config{
		SocketPath: socket,
		AgentPath:  fakeAgent(t, "exit"),
		Workdir:    dir,
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()

	select {
	case err := <-errCh:
		// propogate error
		if err == nil {
			t.Error("expected non-nil error from agent exit code 42")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit after agent exit")
	}

	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("socket file should be removed on shutdown, got err=%v", err)
	}
}

func TestContextCancelShutsDownCleanly(t *testing.T) {
	socket, cancel := startProxy(t, fakeAgent(t, "echo"))

	// Establish a connection so the pump has a current client.
	c := dialRetry(t, socket)
	cr := newConnReader(c)
	writeLine(t, c, "ping")
	_ = readLine(t, cr, 3*time.Second)

	cancel()

	// Connection should be closed by the proxy.
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Error("expected connection to be closed after context cancel")
	}

	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("socket file should be removed on shutdown, got err=%v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{AgentPath: "/x"}); err == nil {
		t.Error("expected error for empty socket path")
	}
	if _, err := New(Config{SocketPath: "/x"}); err == nil {
		t.Error("expected error for empty agent path")
	}
	p, err := New(Config{SocketPath: "/x", AgentPath: "/y"})
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.Workdir != "/data" {
		t.Errorf("default workdir = %q, want /data", p.cfg.Workdir)
	}
}

// Test pump's RW loop and stdin mutex over a sustained session
func TestSequentialLines(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	c := dialRetry(t, socket)
	defer func() { _ = c.Close() }()
	cr := newConnReader(c)

	const n = 20
	for i := 0; i < n; i++ {
		msg := fmt.Sprintf("msg-%02d", i)
		writeLine(t, c, msg)
		if got := readLine(t, cr, 3*time.Second); got != msg {
			t.Errorf("line %d: got %q, want %q", i, got, msg)
		}
	}
}

// Send several lines at once and ensure all echoed correctly
func TestBurstWrite(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	c := dialRetry(t, socket)
	defer func() { _ = c.Close() }()
	cr := newConnReader(c)

	const n = 10
	var burst strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&burst, "burst-%d\n", i)
	}
	if _, err := c.Write([]byte(burst.String())); err != nil {
		t.Fatalf("burst write: %v", err)
	}

	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		line := readLine(t, cr, 5*time.Second)
		seen[line] = true
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("burst-%d", i)
		if !seen[want] {
			t.Errorf("missing %q in responses", want)
		}
	}
}

// Ensure proxy test works with a real bash script
func TestRealAgentBashScript(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "acp-agent")
	// A bash script that reads a line, transforms it, and writes it back.
	script := `#!/usr/bin/env bash
set -euo pipefail
while IFS= read -r line; do
	printf 'ACK:%s\n' "$line"
done
`
	if err := os.WriteFile(agentPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	socket, _ := startProxy(t, agentPath)
	c := dialRetry(t, socket)
	defer func() { _ = c.Close() }()
	cr := newConnReader(c)

	writeLine(t, c, "test")
	if got := readLine(t, cr, 3*time.Second); got != "ACK:test" {
		t.Errorf("got %q, want %q", got, "ACK:test")
	}
}

func TestMultipleEvictionsInSequence(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	for i := 0; i < 5; i++ {
		c := dialRetry(t, socket)
		cr := newConnReader(c)
		msg := fmt.Sprintf("iter-%d", i)
		writeLine(t, c, msg)
		if got := readLine(t, cr, 3*time.Second); got != msg {
			t.Errorf("iter %d: got %q, want %q", i, got, msg)
		}
		_ = c.Close()
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSocketPermissions(t *testing.T) {
	socket, _ := startProxy(t, fakeAgent(t, "echo"))

	// Wait for socket to appear.
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("socket mode = %04o, want 0600", mode)
	}
}

func TestSocketPathRemovedBeforeListen(t *testing.T) {
	// Pre-create a stale socket file; the proxy should remove it and listen.
	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	if err := os.WriteFile(socket, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	socket, _ = startProxyReuseDir(t, dir, fakeAgent(t, "echo"))
	c := dialRetry(t, socket)
	defer func() { _ = c.Close() }()
	cr := newConnReader(c)
	writeLine(t, c, "works")
	if got := readLine(t, cr, 3*time.Second); got != "works" {
		t.Errorf("got %q, want %q", got, "works")
	}
}

// startProxyReuseDir is like startProxy but lets the caller control the temp
// dir in the case of a reexisting UDS
func startProxyReuseDir(t *testing.T, dir, agentPath string) (string, func()) {
	t.Helper()
	socket := filepath.Join(dir, "agent.sock")
	cfg := Config{
		SocketPath: socket,
		AgentPath:  agentPath,
		Workdir:    dir,
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
		}
	})
	return socket, cancel
}

// Ensure no goroutine leaks after a clean shutdown
func TestNoGoroutineLeak(t *testing.T) {
	before := goroutineCount(t)

	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	cfg := Config{
		SocketPath: socket,
		AgentPath:  fakeAgent(t, "echo"),
		Workdir:    dir,
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()

	c := dialRetry(t, socket)
	cr := newConnReader(c)
	writeLine(t, c, "ping")
	_ = readLine(t, cr, 3*time.Second)
	_ = c.Close()
	cancel()
	<-errCh

	// Allow goroutines to settle.
	time.Sleep(300 * time.Millisecond)

	after := goroutineCount(t)
	if after > before+2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

func goroutineCount(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("sh", "-c", "cat /proc/self/status | grep Threads").Output()
	if err != nil {
		t.Skipf("can't read thread count: %v", err)
	}
	var label string
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(out)), "%s %d", &label, &n)
	return n
}
