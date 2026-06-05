package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// RunScan browses the LAN for omnidrop instances advertising via mDNS.
// If mDNS returns no peers, it falls back to direct TCP probing of the
// local subnet (useful on Android where incoming multicast is often
// blocked at the firmware level).
func RunScan(ctx context.Context, timeout time.Duration, asJSON bool) error {
	ifaces, err := UsableInterfaces()
	if err != nil {
		return fmt.Errorf("listing interfaces: %w", err)
	}
	if len(ifaces) == 0 {
		slog.Warn("no usable interfaces found for mDNS scan")
		return nil
	}

	for _, iface := range ifaces {
		slog.Debug("using interface", "name", iface.Name, "flags", iface.Flags)
	}

	// Phase 1: mDNS scan.
	resolver, err := zeroconf.NewResolver(
		zeroconf.SelectIfaces(ifaces),
		zeroconf.SelectIPTraffic(zeroconf.IPv4),
	)
	if err != nil {
		return fmt.Errorf("creating resolver: %w", err)
	}

	store := newPeerStore()
	entries := make(chan *zeroconf.ServiceEntry)
	mdnsDone := make(chan struct{})

	go func() {
		defer close(mdnsDone)
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
	<-mdnsDone

	peers := store.snapshot()

	// Phase 2: if mDNS found nothing, try direct TCP subnet probing.
	if len(peers) == 0 {
		slog.Info("mDNS found no peers, trying direct TCP subnet scan")
		subnetPeers := scanSubnet(ctx, ifaces[0], 500*time.Millisecond)
		store = newPeerStore()
		for _, p := range subnetPeers {
			store.addOrUpdate(p)
		}
		peers = store.snapshot()
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(peers)
	}
	PrintPeersTable(peers)
	return nil
}

// scanSubnet probes every host on the subnet of the given interface for
// an omnidrop banner on the default port (9000). Uses concurrent workers
// for speed.
func scanSubnet(ctx context.Context, iface net.Interface, perHostTimeout time.Duration) []Peer {
	addrs, err := iface.Addrs()
	if err != nil {
		slog.Warn("cannot get interface addresses", "name", iface.Name, "err", err)
		return nil
	}

	// Find the first IPv4 address on the interface to determine the subnet.
	var subnet *net.IPNet
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil {
				subnet = ipnet
				break
			}
		}
	}
	if subnet == nil {
		slog.Warn("no IPv4 address found on interface", "name", iface.Name)
		return nil
	}

	ones, _ := subnet.Mask.Size()
	slog.Info("scanning subnet",
		"network", subnet.IP.Mask(subnet.Mask),
		"mask", fmt.Sprintf("/%d", ones),
		"self", subnet.IP,
	)

	// Generate all host IPs in the subnet.
	hosts := subnetHosts(subnet.IP, subnet.Mask)
	slog.Debug("subnet host count", "count", len(hosts))

	// Concurrent worker pool: probe up to 20 hosts at a time.
	const workers = 20
	ipCh := make(chan net.IP, len(hosts))
	type result struct {
		peer *Peer
		addr string
	}
	resultCh := make(chan result, len(hosts))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ipCh {
				addr := net.JoinHostPort(ip.String(), "9000")
				p, err := ProbePeer(ctx, addr, perHostTimeout)
				if err != nil {
					continue // host not running omnidrop or unreachable
				}
				resultCh <- result{peer: p, addr: addr}
				slog.Info("peer discovered via subnet scan",
					"instance", p.Instance,
					"ipv4", p.IPv4,
					"port", p.Port,
				)
			}
		}()
	}

	// Feed IPs to workers.
	for _, ip := range hosts {
		// Skip our own address.
		if ip.Equal(subnet.IP) {
			continue
		}
		ipCh <- ip
	}
	close(ipCh)
	wg.Wait()
	close(resultCh)

	var peers []Peer
	for r := range resultCh {
		peers = append(peers, *r.peer)
	}
	return peers
}

// subnetHosts returns all usable host IPs in the given subnet, excluding
// the network address and broadcast address.
func subnetHosts(netIP net.IP, mask net.IPMask) []net.IP {
	network := netIP.Mask(mask)
	broadcast := make(net.IP, len(network))
	for i := range network {
		broadcast[i] = network[i] | ^mask[i]
	}

	ones, bits := mask.Size()
	total := 1 << (bits - ones)
	if total <= 2 {
		return nil // no usable hosts in a /31 or /32
	}

	hosts := make([]net.IP, 0, total-2)
	for i := 1; i < total-1; i++ {
		ip := make(net.IP, len(network))
		copy(ip, network)
		// Add the host offset, carrying across bytes.
		carry := i
		for j := len(ip) - 1; j >= 0 && carry > 0; j-- {
			sum := int(ip[j]) + carry
			ip[j] = byte(sum & 0xff)
			carry = sum >> 8
		}
		// Skip network and broadcast.
		if ip.Equal(network) || ip.Equal(broadcast) {
			continue
		}
		hosts = append(hosts, ip)
	}
	return hosts
}
