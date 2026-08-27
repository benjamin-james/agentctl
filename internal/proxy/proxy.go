// Detachable ACP proxy
//
// The proxy owns a long-lived agent process and exposes a Unix domain socket.
// At most one client is attached at a time; a new connection evicts the
// previous one. While no client is attached the agent's stdout applies
// backpressure, where the agent blocks on its next write so no data is lost and
// no output is buffered unboundedly.
//
// Clients connect to the UDS (i.e. socat). The proxy
// bridges client↔agent bidirectionally. Killing a client does not kill the
// agent because the agent is a child of the proxy daemon, not of the client's
// SSH session.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	// UDS path. 600 perm, parent 700
	SocketPath string
	// exec to run as agent
	AgentPath string
	// flexibility but default /data/
	Workdir string
	// environment, if nil inherits proxy's env
	Env []string
}

// Proxy owns the agent process and the UDS listener. Use New to construct
// one, then call Run.
type Proxy struct {
	cfg Config

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	listener net.Listener

	attachCh chan attachReq
	stdinMu  sync.Mutex

	agentDone chan struct{}
}

// attachReq is sent from a client handler to the pump. The pump closes
// req.done once the swap is complete so the handler knows it can start the
// client→agent copy.
type attachReq struct {
	conn net.Conn
	done chan struct{}
}

const (
	writeTimeout = 30 * time.Second
	readBufSize  = 32 * 1024
)

// New proxy. Doesn't invoke run automatically.
func New(cfg Config) (*Proxy, error) {
	if cfg.SocketPath == "" {
		return nil, errors.New("proxy: empty socket path")
	}
	if cfg.AgentPath == "" {
		return nil, errors.New("proxy: empty agent path")
	}
	if cfg.Workdir == "" {
		cfg.Workdir = "/data"
	}
	return &Proxy{
		cfg:       cfg,
		attachCh:  make(chan attachReq),
		agentDone: make(chan struct{}),
	}, nil
}

func (p *Proxy) AgentPID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Run starts the agent, opens the UDS, and serves until either:
//   - ctx is cancelled (clean shutdown),
//   - the agent exits on its own (inherits exit error).
//
// On shutdown the listener is closed, the agent is sent SIGTERM,
// pump drained, and the socket file is removed.
func (p *Proxy) Run(ctx context.Context) error {
	if err := p.startAgent(); err != nil {
		return err
	}

	var waitErr error
	go func() {
		waitErr = p.cmd.Wait()
		close(p.agentDone)
	}()

	// On ctx cancellation send SIGTERM to the agent so it
	// can shut down gracefully
	go func() {
		<-ctx.Done()
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		}
	}()

	if err := p.listen(); err != nil {
		return err
	}

	pumpDone := make(chan struct{})
	go p.pump(pumpDone)

	// Accept loop runs concurrently and exits when the listener closes.
	go p.acceptLoop()

	select {
	case <-ctx.Done():
		p.listener.Close()
		_ = p.stdin.Close()
		<-pumpDone
		<-p.agentDone
		_ = os.Remove(p.cfg.SocketPath)
		return nil
	case <-p.agentDone:
		p.listener.Close()
		<-pumpDone
		_ = os.Remove(p.cfg.SocketPath)
		return waitErr
	}
}

func (p *Proxy) startAgent() error {
	c := exec.Command(p.cfg.AgentPath)
	c.Dir = p.cfg.Workdir
	if p.cfg.Env != nil {
		c.Env = p.cfg.Env
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := c.StdinPipe()
	if err != nil {
		return fmt.Errorf("proxy: stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return fmt.Errorf("proxy: stdout pipe: %w", err)
	}
	c.Stderr = log.Writer()
	if err := c.Start(); err != nil {
		return fmt.Errorf("proxy: start agent: %w", err)
	}
	p.cmd = c
	p.stdin = stdin
	p.stdout = stdout
	return nil
}

func (p *Proxy) listen() error {
	if err := os.MkdirAll(filepath.Dir(p.cfg.SocketPath), 0700); err != nil {
		return fmt.Errorf("proxy: mkdir socket dir: %w", err)
	}
	_ = os.Remove(p.cfg.SocketPath) // remove stale socket from a previous run
	l, err := net.Listen("unix", p.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("proxy: listen: %w", err)
	}
	p.listener = l
	if err := os.Chmod(p.cfg.SocketPath, 0600); err != nil {
		return fmt.Errorf("proxy: chmod socket: %w", err)
	}
	return nil
}

func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

// handle processes a single client connection: requests attachment to the
// pump, then runs the client to agent copy until the conn is closed (by client
// disconnect, eviction, or agent exit).
func (p *Proxy) handle(conn net.Conn) {
	defer conn.Close()
	req := attachReq{conn: conn, done: make(chan struct{})}
	select {
	case p.attachCh <- req:
	case <-p.agentDone:
		return
	}
	select {
	case <-req.done:
	case <-p.agentDone:
		return
	}

	buf := make([]byte, readBufSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			p.stdinMu.Lock()
			_, werr := p.stdin.Write(buf[:n])
			p.stdinMu.Unlock()
			if werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Pump reads chunks from agent's stdout via reader goroutine
// and writes to current client so can be the effective owner of the
// current client. When no client is attached the goroutine blocks
// which blocks the agent's stdout pipe.
func (p *Proxy) pump(done chan<- struct{}) {
	defer close(done)

	type chunk struct {
		data []byte
		err  error
	}
	agentCh := make(chan chunk)

	// Drain agent stdout. Blocks on agentCh send when
	// pump isn't consuming (no client) -> agent pipe fills ->
	// agent blocks.
	go func() {
		buf := make([]byte, readBufSize)
		for {
			n, err := p.stdout.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				select {
				case agentCh <- chunk{data: cp}:
				case <-p.agentDone:
					return
				}
			}
			if err != nil {
				select {
				case agentCh <- chunk{err: err}:
				case <-p.agentDone:
				}
				return
			}
		}
	}()

	var current net.Conn
	for {
		if current == nil {
			select {
			case req := <-p.attachCh:
				current = req.conn
				close(req.done)
			case <-p.agentDone:
				return
			}
			continue
		}

		select {
		case req := <-p.attachCh:
			// Evict: close old conn, swap to new.
			current.Close()
			current = req.conn
			close(req.done)
		case <-p.agentDone:
			current.Close()
			return
		case ch := <-agentCh:
			if ch.err != nil {
				current.Close()
				return
			}
			_ = current.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := current.Write(ch.data); err != nil {
				current.Close()
				current = nil
			}
		}
	}
}
