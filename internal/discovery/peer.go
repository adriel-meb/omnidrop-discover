package discovery

import (
	"fmt"
	"os"
	"sort"
	"strings"
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

// splitComma splits s on ',' and returns non-empty segments.
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

// ─── Peer results table ───────────────────────────────────────────────────────

// PrintPeersTable prints discovered peers as a modern, colorful box-drawn table
// to stderr.
func PrintPeersTable(peers []Peer) {
	if len(peers) == 0 {
		fmt.Fprintln(os.Stderr, "\033[33m  ◎  no peers found\033[0m")
		return
	}

	// ─── Build rows ──────────────────────────────────────────────────────
	headers := []string{"INSTANCE", "IPV4", "PORT", "PLATFORM", "VERSION"}
	rows := make([][]string, len(peers))
	for i, p := range peers {
		ipv4 := ""
		if len(p.IPv4) > 0 {
			ipv4 = p.IPv4[0]
		}
		rows[i] = []string{
			truncate(p.Instance, 36),
			ipv4,
			fmt.Sprintf("%d", p.Port),
			p.Platform,
			p.Version,
		}
	}

	// ─── Column widths ───────────────────────────────────────────────────
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}
	padded := make([]int, len(colWidths))
	for i, w := range colWidths {
		padded[i] = w + 2
	}

	// ─── Separator builder ───────────────────────────────────────────────
	hline := func(left, mid, right, fill string) string {
		var b strings.Builder
		b.WriteString(left)
		for i, w := range padded {
			if i > 0 {
				b.WriteString(mid)
			}
			b.WriteString(strings.Repeat(fill, w))
		}
		b.WriteString(right)
		return b.String()
	}

	out := os.Stderr

	// ╭── Top border (rounded) ──────────────────────────────────────────
	fmt.Fprintln(out, dim(hline("╭", "┬", "╮", "─")))

	// │  Header row — bold white on subtle background
	headerBg := "\033[48;5;236m\033[97m"
	headerReset := "\033[0m"
	fmt.Fprint(out, headerBg+" │"+headerReset)
	for i, h := range headers {
		fmt.Fprintf(out, "%s %s%s%s │",
			headerBg, boldWhite, padRight(h, colWidths[i]), headerReset,
		)
	}
	fmt.Fprintln(out)

	// ├── Header separator
	fmt.Fprintln(out, dim(hline("├", "┼", "┤", "─")))

	// │  Data rows — alternating subtle background shades
	for idx, row := range rows {
		// Alternating row backgrounds: dark grey for odd, slightly lighter for even
		rowBg := "\033[48;5;235m"
		if idx%2 == 0 {
			rowBg = "\033[48;5;237m"
		}
		fmt.Fprint(out, rowBg+" │"+resetANSI)
		for i, cell := range row {
			colored := tableColor(i, padRight(cell, colWidths[i]))
			fmt.Fprintf(out, " %s │", rowBg+colored+resetANSI)
		}
		fmt.Fprintln(out)
	}

	// ╰── Bottom border (rounded)
	fmt.Fprintln(out, dim(hline("╰", "┴", "╯", "─")))

	// ─── Footer summary line ────────────────────────────────────────────
	noun := "peer"
	if len(peers) != 1 {
		noun = "peers"
	}
	fmt.Fprintf(out, "  \033[2m%s %d %s discovered\033[0m\n", "◆", len(peers), noun)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// ─── ANSI helpers ─────────────────────────────────────────────────────────────

const (
	boldWhite = "\033[1;97m"
	resetANSI = "\033[0m"
)

func dim(s string) string  { return "\033[2m" + s + "\033[0m" }
func yellow(s string) string { return "\033[33m" + s + "\033[0m" }

// tableColor returns a color-wrapped cell value for the given column index.
func tableColor(col int, s string) string {
	switch col {
	case 0:
		return "\033[1;97m" + s + "\033[0m" // instance — bright white bold
	case 1:
		return "\033[38;5;83m" + s + "\033[0m" // IP — bright green (256-color)
	case 2:
		return "\033[38;5;221m" + s + "\033[0m" // port — warm yellow
	case 3:
		return "\033[38;5;117m" + s + "\033[0m" // platform — soft cyan
	case 4:
		return "\033[38;5;219m" + s + "\033[0m" // version — soft magenta
	default:
		return s
	}
}
