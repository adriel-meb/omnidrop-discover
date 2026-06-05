package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/grandcat/zeroconf"
)

// RunScan browses the LAN for omnidrop instances advertising via mDNS.
// It listens for responses until the timeout expires, then prints the
// discovered peers (as a table or JSON) and returns.
func RunScan(ctx context.Context, timeout time.Duration, asJSON bool) error {
	// Enumerate suitable interfaces ourselves so we can log them and fall
	// back gracefully on platforms where net.Interfaces() doesn't report
	// FlagMulticast (e.g. Android/Termux).
	ifaces, err := UsableInterfaces()
	if err != nil {
		slog.Warn("enumerating interfaces", "err", err)
	}
	if len(ifaces) == 0 {
		slog.Warn("no multicast-capable interfaces found, trying all up interfaces")
		all, listErr := net.Interfaces()
		if listErr != nil {
			return fmt.Errorf("listing interfaces: %w", listErr)
		}
		for _, iface := range all {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			ifaces = append(ifaces, iface)
		}
	}

	for _, iface := range ifaces {
		slog.Debug("using interface", "name", iface.Name, "flags", iface.Flags)
	}

	// Create a zeroconf resolver on the selected interfaces.
	resolver, err := zeroconf.NewResolver(
		zeroconf.SelectIfaces(ifaces),
		zeroconf.SelectIPTraffic(zeroconf.IPv4),
	)
	if err != nil {
		return fmt.Errorf("creating resolver: %w", err)
	}

	store := newPeerStore()
	entries := make(chan *zeroconf.ServiceEntry)
	done := make(chan struct{})

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

	browseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := resolver.Browse(browseCtx, serviceType, serviceDomain, entries); err != nil {
		return fmt.Errorf("browsing: %w", err)
	}

	<-browseCtx.Done()
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
