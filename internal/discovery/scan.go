package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/grandcat/zeroconf"
)

// RunScan browses the LAN for omnidrop instances advertising via mDNS.
// It listens for responses until the timeout expires, then prints the
// discovered peers (as a table or JSON) and returns.
func RunScan(ctx context.Context, timeout time.Duration, asJSON bool) error {
	// Create a zeroconf resolver that listens on all multicast-capable
	// interfaces for both IPv4 and IPv6 mDNS traffic.
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("creating resolver: %w", err)
	}

	store := newPeerStore()
	// entries is an unbuffered channel. The zeroconf library's mainloop
	// sends discovered ServiceEntry values here, and our goroutine below
	// converts them to Peer structs.
	entries := make(chan *zeroconf.ServiceEntry)
	done := make(chan struct{})

	// Background goroutine that drains the entries channel and populates
	// the peerStore. Runs until entries is closed by the library (which
	// happens when the browse context is cancelled).
	go func() {
		defer close(done)
		for entry := range entries {
			p := entryToPeer(entry)
			parseTXT(&p)
			if store.addOrUpdate(p) {
				slog.Info("peer discovered",
					"instance", p.Instance,
					"ipv4", p.IPv4,
					"port", p.Port,
					"version", p.Version,
					"platform", p.Platform,
				)
			} else {
				slog.Debug("peer updated", "instance", p.Instance)
			}
		}
	}()

	// Create a derived context with the desired scan timeout. When it
	// expires, Browse's mainloop will be cancelled and the entries
	// channel will be closed.
	browseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := resolver.Browse(browseCtx, serviceType, serviceDomain, entries); err != nil {
		return fmt.Errorf("browsing: %w", err)
	}

	// Wait for the timeout to expire.
	<-browseCtx.Done()
	// Wait for the consumer goroutine to finish processing remaining entries.
	<-done

	peers := store.snapshot()
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(peers)
	}
	PrintPeersTable(peers)
	return nil
}
