package discovery

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// UsableInterfaces returns network interfaces that are up, not loopback,
// and support multicast — the ones suitable for LAN mDNS discovery.
//
// Uses a tiered approach to handle restricted environments (Android/Termux):
//  1. net.Interfaces()    — netlink, works on desktop
//  2. ifconfig (exec)     — ioctl-based, often works on Android when netlink is blocked
func UsableInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		slog.Debug("net.Interfaces() failed, trying ifconfig fallback", "err", err)
		all = ifconfigInterfaces()
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

// ifconfigInterfaces runs "ifconfig" and parses its output to build
// net.Interface values. This uses ioctl under the hood, which is
// typically allowed on Android when netlink and procfs are blocked.
func ifconfigInterfaces() []net.Interface {
	cmd := exec.Command("ifconfig", "-a")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("ifconfig failed", "err", err)
		return nil
	}

	var out []net.Interface
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()

		// Parse: "wlan0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500"
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}
		rest := line[colonIdx+1:]
		if !strings.HasPrefix(strings.TrimSpace(rest), "flags=") {
			continue
		}

		name := strings.TrimSpace(line[:colonIdx])

		// Extract decimal flags value after "flags=".
		flagsPart := strings.TrimSpace(rest)
		flagsPart = strings.TrimPrefix(flagsPart, "flags=")
		flagsEnd := strings.Index(flagsPart, "<")
		if flagsEnd == -1 {
			flagsEnd = strings.Index(flagsPart, " ")
		}
		if flagsEnd == -1 {
			continue
		}
		flagsStr := flagsPart[:flagsEnd]
		flagsInt, err := strconv.ParseUint(flagsStr, 10, 32)
		if err != nil {
			slog.Debug("parsing flags", "name", name, "val", flagsStr, "err", err)
			continue
		}

		// Extract MTU.
		mtu := 0
		mtuIdx := strings.Index(line, " mtu ")
		if mtuIdx >= 0 {
			mtuStr := strings.Fields(line[mtuIdx+5:])
			if len(mtuStr) > 0 {
				mtu, _ = strconv.Atoi(mtuStr[0])
			}
		}

		out = append(out, net.Interface{
			Index: len(out) + 1,
			MTU:   mtu,
			Name:  name,
			Flags: net.Flags(flagsInt),
		})
	}
	return out
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
