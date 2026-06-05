package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/grandcat/zeroconf"
)

// RunServe advertises this instance on the LAN via mDNS and starts the TCP
// banner server. It blocks until ctx is cancelled or the mDNS registration
// fails.
func RunServe(ctx context.Context, port int) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("getting hostname: %w", err)
	}

	// Start the TCP banner server so that peers can fetch structured metadata
	// (version, platform, capabilities) without relying solely on DNS TXT
	// records (which have a 255-byte limit per entry).
	if err := startBannerServer(ctx, port); err != nil {
		return fmt.Errorf("banner server: %w", err)
	}

	// Build TXT records. These are served alongside the mDNS PTR/SRV
	// responses so that scanning peers get basic metadata without having
	// to probe each host individually.
	txt := []string{
		"version=" + appVersion,
		"platform=" + runtime.GOOS + "/" + runtime.GOARCH,
		"caps=files,folders",
	}

	// Register the service with Zeroconf (Bonjour/Avahi). This sends
	// multicast announcements and responds to probes on the LAN.
	server, err := zeroconf.Register(
		hostname,    // instance name
		serviceType, // _omnidrop._tcp
		serviceDomain,
		port,
		txt,
		nil, // use all multicast-capable interfaces
	)
	if err != nil {
		return fmt.Errorf("registering service: %w", err)
	}
	// Shutdown the mDNS server when RunServe returns. Note that
	// server.Shutdown() also sends a "goodbye" packet (TTL=0) so
	// that caches on other hosts are flushed promptly.
	defer server.Shutdown()

	slog.Info("advertising service",
		"instance", hostname,
		"type", serviceType,
		"port", port,
		"version", appVersion,
		"platform", runtime.GOOS,
	)
	fmt.Println("press Ctrl+C to stop")

	// Block until the caller cancels the context (e.g. SIGINT/SIGTERM).
	<-ctx.Done()
	slog.Info("shutting down")
	return nil
}
