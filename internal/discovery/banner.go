package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"time"
)

// Banner is the JSON structure sent by the TCP banner server when a peer
// connects. It carries structured metadata that cannot always fit into
// DNS TXT records (which are limited to 255 bytes per string).
type Banner struct {
	Instance string   `json:"instance"`
	Version  string   `json:"version"`
	Platform string   `json:"platform"`
	Caps     []string `json:"caps"`
	Port     int      `json:"port"`
}

// startBannerServer listens on the given TCP port and serves a one-line JSON
// banner to every connecting peer. The server runs in background goroutines
// and is torn down when ctx is cancelled.
func startBannerServer(ctx context.Context, port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen tcp :%d: %w", port, err)
	}

	hostname, _ := os.Hostname()
	banner := Banner{
		Instance: hostname,
		Version:  appVersion,
		Platform: runtime.GOOS,
		Caps:     []string{"files", "folders"},
		Port:     port,
	}
	bannerJSON, err := json.Marshal(banner)
	if err != nil {
		ln.Close()
		return fmt.Errorf("marshal banner: %w", err)
	}
	bannerJSON = append(bannerJSON, '\n')

	// Goroutine: close the listener when the context is cancelled, which
	// causes Accept() below to return and the accept loop to exit.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	// Goroutine: accept loop. Each accepted connection gets a short-lived
	// goroutine that writes the banner and closes.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// If the context is done, the listener was closed by the
				// goroutine above — this is the expected shutdown path.
				if ctx.Err() != nil {
					return
				}
				slog.Warn("banner accept failed", "err", err)
				continue
			}
			go handleBannerConn(conn, bannerJSON)
		}
	}()

	slog.Info("banner server listening", "port", port)
	return nil
}

// handleBannerConn sends the banner JSON to conn and closes it. A short
// write deadline prevents a slow (or malicious) peer from holding the
// connection open.
func handleBannerConn(conn net.Conn, banner []byte) {
	defer conn.Close()
	// Give the peer 2 seconds to read the banner.
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		slog.Debug("set write deadline failed", "remote", conn.RemoteAddr(), "err", err)
		return
	}
	if _, err := conn.Write(banner); err != nil {
		slog.Debug("banner write failed", "remote", conn.RemoteAddr(), "err", err)
	}
}

// ProbePeer connects to addr (in "host:port" form), reads a JSON banner,
// and returns the parsed Peer. It is used for out-of-band probing outside
// of mDNS, e.g. when a user specifies a specific host to query.
func ProbePeer(ctx context.Context, addr string, timeout time.Duration) (*Peer, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read banner: %w", err)
	}

	var b Banner
	if err := json.Unmarshal(line, &b); err != nil {
		return nil, fmt.Errorf("parse banner: %w", err)
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("parse addr %q: %w", addr, err)
	}

	return &Peer{
		Instance: b.Instance,
		Host:     host,
		Port:     b.Port,
		IPv4:     []string{host},
		Version:  b.Version,
		Platform: b.Platform,
		Caps:     b.Caps,
	}, nil
}
