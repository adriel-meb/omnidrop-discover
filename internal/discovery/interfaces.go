package discovery

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
)

// UsableInterfaces returns network interfaces that are up, not loopback,
// and support multicast — the ones suitable for LAN mDNS discovery.
//
// Uses a tiered approach to handle restricted environments (Android/Termux):
//  1. net.Interfaces()      — netlink, works on desktop
//  2. /proc/net/dev + ioctl — works on most Android devices
//  3. ip link show (exec)   — last resort when ioctl is also blocked
func UsableInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		slog.Debug("net.Interfaces() failed, trying fallback", "err", err)
		all = fallbackInterfaces()
	}

	var out []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		out = append(out, iface)
	}
	return out, nil
}

// fallbackInterfaces tries progressively less-restricted methods to enumerate
// network interfaces on platforms where net.Interfaces() is blocked.
func fallbackInterfaces() []net.Interface {
	// Tier 1: parse /proc/net/dev for names, then use ioctl via InterfaceByName.
	if ifaces := procNetDevInterfaces(); len(ifaces) > 0 {
		return ifaces
	}

	// Tier 2: shell out to ip link show as a last resort.
	if ifaces := ipLinkInterfaces(); len(ifaces) > 0 {
		return ifaces
	}

	slog.Warn("all interface enumeration methods failed")
	return nil
}

// procNetDevInterfaces reads interface names from /proc/net/dev and resolves
// each via net.InterfaceByName() (ioctl-based, often allowed when netlink is
// blocked on Android).
func procNetDevInterfaces() []net.Interface {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		slog.Debug("/proc/net/dev unavailable", "err", err)
		return nil
	}
	defer f.Close()

	var names []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNo++
		if lineNo <= 2 {
			continue // skip header and separator
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}
		names = append(names, strings.TrimSpace(line[:colonIdx]))
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("reading /proc/net/dev", "err", err)
		return nil
	}

	var out []net.Interface
	for _, name := range names {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			slog.Debug("InterfaceByName failed", "name", name, "err", err)
			continue
		}
		out = append(out, *iface)
	}
	return out
}

// ipLinkInterfaces runs "ip -o link show" and parses the output to build
// net.Interface values. Used as a last resort on restricted platforms.
func ipLinkInterfaces() []net.Interface {
	cmd := exec.Command("ip", "-o", "link", "show")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("ip link show failed", "err", err)
		return nil
	}

	// Parse lines like:
	// 1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 ...
	// 3: wlan0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 ...
	var out []net.Interface
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Extract the interface name (between first colon and the next colon/space).
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[1])

		// Extract flags from <...>.
		flagsStr := extractIPFlags(line)
		flags := parseIPFlags(flagsStr)

		// Extract MTU.
		mtu := 0
		if m := extractIPField(line, "mtu"); m != "" {
			fmt.Sscanf(m, "%d", &mtu)
		}

		out = append(out, net.Interface{
			Name:  name,
			Flags: flags,
			MTU:   mtu,
		})
	}
	return out
}

// extractIPFlags extracts the flag string between < and > from an ip link line.
func extractIPFlags(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return line[start+1 : end]
}

// parseIPFlags converts ip link flag names (e.g. "UP,BROADCAST,MULTICAST")
// into a net.Flags bitmask.
func parseIPFlags(s string) net.Flags {
	var flags net.Flags
	for _, f := range strings.Split(s, ",") {
		switch strings.TrimSpace(f) {
		case "UP":
			flags |= net.FlagUp
		case "BROADCAST":
			flags |= net.FlagBroadcast
		case "LOOPBACK":
			flags |= net.FlagLoopback
		case "POINTOPOINT":
			flags |= net.FlagPointToPoint
		case "MULTICAST":
			flags |= net.FlagMulticast
		}
	}
	return flags
}

// extractIPField extracts the value after a named field from an ip link line.
// e.g. extractIPField("... mtu 1500 ...", "mtu") -> "1500"
func extractIPField(line, field string) string {
	idx := strings.Index(line, " "+field+" ")
	if idx == -1 {
		return ""
	}
	rest := line[idx+len(field)+2:]
	parts := strings.Fields(rest)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// DescribeInterface returns a human-readable summary like
// "en0 [192.168.1.20, fe80::1]".
func DescribeInterface(iface net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return fmt.Sprintf("%s [error: %v]", iface.Name, err)
	}

	var ips []string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return fmt.Sprintf("%s [%s]", iface.Name, strings.Join(ips, ", "))
}
