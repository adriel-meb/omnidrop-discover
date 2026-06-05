package discovery

import (
	"fmt"
	"sort"
	"sync"

	"github.com/grandcat/zeroconf"
)

// Peer represents a discovered omnidrop instance on the LAN.
type Peer struct {
	Instance string   `json:"instance"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	IPv4     []string `json:"ipv4,omitempty"`
	IPv6     []string `json:"ipv6,omitempty"`
	TXT      []string `json:"txt,omitempty"`
	Version  string   `json:"version,omitempty"`
	Platform string   `json:"platform,omitempty"`
	Caps     []string `json:"caps,omitempty"`
}

// peerStore is a concurrency-safe collection of discovered peers, keyed by
// "instance|port" so that the same service instance on the same port is
// deduplicated. When a new mDNS response arrives for a known key the record
// is silently replaced (updated).
type peerStore struct {
	mu    sync.Mutex
	peers map[string]Peer
}

func newPeerStore() *peerStore {
	return &peerStore{peers: make(map[string]Peer)}
}

// addOrUpdate inserts or replaces p. It returns true when the peer is new
// (first seen) and false when an existing record was updated.
func (s *peerStore) addOrUpdate(p Peer) (added bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s|%d", p.Instance, p.Port)
	_, exists := s.peers[key]
	s.peers[key] = p
	return !exists
}

// snapshot returns a copy of all stored peers sorted by instance name.
func (s *peerStore) snapshot() []Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out
}

// entryToPeer converts a zeroconf.ServiceEntry into a Peer, copying IP
// addresses as human-readable strings.
func entryToPeer(e *zeroconf.ServiceEntry) Peer {
	p := Peer{
		Instance: e.Instance,
		Host:     e.HostName,
		Port:     e.Port,
		TXT:      e.Text,
	}
	for _, ip := range e.AddrIPv4 {
		p.IPv4 = append(p.IPv4, ip.String())
	}
	for _, ip := range e.AddrIPv6 {
		p.IPv6 = append(p.IPv6, ip.String())
	}
	return p
}

// parseTXT extracts well-known TXT-record keys (version, platform, caps)
// into the corresponding Peer fields. Unknown keys are silently ignored.
func parseTXT(p *Peer) {
	for _, kv := range p.TXT {
		// Find the first '=' separator. If there is none, skip this entry.
		eq := -1
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				eq = i
				break
			}
		}
		if eq < 0 {
			continue
		}
		k, v := kv[:eq], kv[eq+1:]
		switch k {
		case "version":
			p.Version = v
		case "platform":
			p.Platform = v
		case "caps":
			p.Caps = splitComma(v)
		}
	}
}

// splitComma splits s on ',' and returns non-empty segments. It avoids
// allocating strings.Split's empty-string tail for trailing commas.
func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// PrintPeersTable prints a human-readable table of peers to stdout.
func PrintPeersTable(peers []Peer) {
	if len(peers) == 0 {
		fmt.Println("no peers found")
		return
	}
	fmt.Printf("%-25s %-20s %-6s %-10s %-10s\n",
		"INSTANCE", "IPV4", "PORT", "PLATFORM", "VERSION")
	for _, p := range peers {
		ipv4 := ""
		if len(p.IPv4) > 0 {
			ipv4 = p.IPv4[0]
		}
		fmt.Printf("%-25s %-20s %-6d %-10s %-10s\n",
			p.Instance, ipv4, p.Port, p.Platform, p.Version)
	}
}
