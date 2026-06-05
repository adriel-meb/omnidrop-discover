package discovery

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UsableInterfaces returns network interfaces that are up, not loopback,
// and support multicast — the ones suitable for LAN mDNS discovery.
//
// On platforms where net.Interfaces() is restricted (e.g. Android/Termux),
// it falls back to reading /sys/class/net/ instead.
func UsableInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		slog.Debug("net.Interfaces() failed, trying sysfs fallback", "err", err)
		all = sysfsInterfaces()
	}

	var out []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface is down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // skip loopback — mDNS on loopback is not useful for LAN discovery
		}
		if iface.Flags&net.FlagMulticast == 0 {
			continue // multicast required for mDNS
		}
		out = append(out, iface)
	}
	return out, nil
}

// sysfsInterfaces reads network interface information from /sys/class/net/,
// which is typically accessible even when netlink-based net.Interfaces() is
// blocked (e.g. Android/Termux).
func sysfsInterfaces() []net.Interface {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		slog.Warn("sysfs fallback failed", "err", err)
		return nil
	}

	var out []net.Interface
	for _, entry := range entries {
		name := entry.Name()
		iface := readSysfsIface(name)
		if iface != nil {
			out = append(out, *iface)
		}
	}
	return out
}

// readSysfsIface reads a single interface from /sys/class/net/<name>/.
// Returns nil if the interface directory doesn't contain the expected files.
func readSysfsIface(name string) *net.Interface {
	base := filepath.Join("/sys/class/net", name)

	flags, err := readSysfsHex(filepath.Join(base, "flags"))
	if err != nil {
		slog.Debug("skipping interface (no flags)", "name", name, "err", err)
		return nil
	}

	mtu, err := readSysfsInt(filepath.Join(base, "mtu"))
	if err != nil {
		mtu = 0 // non-fatal
	}

	// /sys/class/net/<name>/ifindex
	index := 0
	if indexData, err := os.ReadFile(filepath.Join(base, "ifindex")); err == nil {
		index, _ = strconv.Atoi(strings.TrimSpace(string(indexData)))
	}

	return &net.Interface{
		Index:        index,
		MTU:          mtu,
		Name:         name,
		Flags:        net.Flags(flags),
		HardwareAddr: nil, // not needed for our filtering
	}
}

func readSysfsHex(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 0, 64)
}

func readSysfsInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
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
