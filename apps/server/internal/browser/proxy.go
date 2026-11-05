// Package browser drives a headless Chromium for the model's browser tool
// (docs/design/browser-plan.md): one isolated browser context per
// conversation, reached over CDP, its every request through Proxy.
package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Proxy is the browser's only way out: an HTTP proxy (CONNECT for HTTPS,
// plain forwarding for HTTP) that dials public addresses only, checked at
// dial time like tools.GuardedClient, on ports 80 and 443, to hosts the
// blocklist and Refuse let through. HTTPS is not opened: the proxy sees the
// host it is asked for, and the address it dials.
type Proxy struct {
	// Source says whether a connection may come from addr: the browser's.
	// Anything else is turned away, or the proxy would be an open door to the
	// internet for whoever reaches its port. Loopback always may; nil allows
	// nothing else.
	Source func(addr netip.Addr) bool
	// Blocklist is the site's blocked sites, read at each connection.
	Blocklist func() tools.Blocklist
	// Refuse names hosts never to reach whatever their address: OpenChat's own
	// and the model channels' (a page talking to them is a way out for data).
	Refuse func(host string) bool
	// Dial is for tests; nil dials the internet through the public-address guard.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)

	mu       sync.Mutex
	listener net.Listener
}

const (
	proxyDialTimeout = 10 * time.Second
	proxyIdleTimeout = 2 * time.Minute
)

var errRefused = errors.New("browser: destination refused")

// Listen serves the proxy on addr until Close.
func (p *Proxy) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.listener = ln
	p.mu.Unlock()
	go p.serve(ln)
	return nil
}

// Addr is where the proxy listens, once it does.
func (p *Proxy) Addr() net.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return nil
	}
	return p.listener.Addr()
}

// Close stops listening; connections in flight finish on their own.
func (p *Proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener == nil {
		return nil
	}
	err := p.listener.Close()
	p.listener = nil
	return err
}

func (p *Proxy) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

// allowedSource reports whether a connection comes from the browser.
func (p *Proxy) allowedSource(remote net.Addr) bool {
	tcp, ok := remote.(*net.TCPAddr)
	if !ok {
		return false
	}
	addr, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	return p.Source != nil && p.Source(addr)
}

func (p *Proxy) handle(conn net.Conn) {
	defer conn.Close()
	if !p.allowedSource(conn.RemoteAddr()) {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(proxyDialTimeout * 3))
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	host, port := req.URL.Hostname(), req.URL.Port()
	if req.Method == http.MethodConnect {
		host, port, err = net.SplitHostPort(req.Host)
		if err != nil {
			writeStatus(conn, http.StatusBadRequest)
			return
		}
	} else if port == "" {
		port = "80"
	}
	if !p.permitted(host, port, req.URL.Scheme) {
		writeStatus(conn, http.StatusForbidden)
		return
	}
	upstream, err := p.dial(context.Background(), net.JoinHostPort(host, port))
	if err != nil {
		writeStatus(conn, http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	_ = conn.SetDeadline(time.Time{})

	if req.Method == http.MethodConnect {
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		pipe(conn, reader, upstream)
		return
	}
	// Plain HTTP: the request goes on as the origin form, and the rest of the
	// connection is relayed as it comes.
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	if err := req.Write(upstream); err != nil {
		return
	}
	pipe(conn, reader, upstream)
}

// permitted is the check made before dialling: a web port, a host the site
// has not blocked and that is not refused outright.
func (p *Proxy) permitted(host, port, scheme string) bool {
	if host == "" || (port != "80" && port != "443") {
		return false
	}
	if p.Refuse != nil && p.Refuse(strings.ToLower(host)) {
		return false
	}
	if p.Blocklist != nil {
		target := "https://" + host + "/"
		if scheme == "http" {
			target = "http://" + host + "/"
		}
		if p.Blocklist().Blocks(target) {
			return false
		}
	}
	return true
}

func (p *Proxy) dial(ctx context.Context, address string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, "tcp", address)
	}
	dialer := &net.Dialer{
		Timeout: proxyDialTimeout,
		// The address actually dialled is checked, after resolution: a name
		// that resolves to the internal network, or rebinds to it, is refused.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errRefused
			}
			addr, err := netip.ParseAddr(host)
			if err != nil || !tools.PublicAddress(addr) {
				return errRefused
			}
			return nil
		},
	}
	return dialer.DialContext(ctx, "tcp", address)
}

// pipe relays both ways until either side is done or idle too long.
func pipe(client net.Conn, buffered *bufio.Reader, upstream net.Conn) {
	done := make(chan struct{}, 2)
	idle := func(c net.Conn) { _ = c.SetDeadline(time.Now().Add(proxyIdleTimeout)) }
	go func() {
		copyIdle(upstream, buffered, client, idle)
		done <- struct{}{}
	}()
	go func() {
		copyIdle(client, upstream, upstream, idle)
		done <- struct{}{}
	}()
	<-done
}

func copyIdle(dst net.Conn, src io.Reader, srcConn net.Conn, idle func(net.Conn)) {
	buf := make([]byte, 32<<10)
	for {
		idle(srcConn)
		n, err := src.Read(buf)
		if n > 0 {
			idle(dst)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				slog.Debug("browser proxy relay ended", "error", err)
			}
			return
		}
	}
}

// RefusedHeader marks a response that is the proxy's refusal, not the site's.
const RefusedHeader = "X-Openchat-Refused"

func writeStatus(conn net.Conn, status int) {
	_, _ = io.WriteString(conn, fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, http.StatusText(status)))
	_, _ = io.WriteString(conn, RefusedHeader+": 1\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
}
