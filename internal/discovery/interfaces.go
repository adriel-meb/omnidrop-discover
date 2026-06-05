package discovery

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// Kernel-level interface flags (from <linux/if.h>). These differ from Go's
// net.Flags constants — see kernelFlagsToGo() for the mapping.
const (
	kernelIffUp        = 0x1
	kernelIffBroadcast = 0x2
	kernelIffLoopback  = 0x8
	kernelIffMulticast = 0x1000
	kernelIffRunning   = 0x40
)

// kernelFlagsToGo converts Linux kernel interface flags (as shown by ifconfig)
// to Go's net.Flags bitmask. The kernel and Go use different bit positions.
func kernelFlagsToGo(kflags uint64) net.Flags {
	var gflags net.Flags
	if kflags&kernelIffUp != 0 {
		gflags |= net.FlagUp
	}
	if kflags&kernelIffBroadcast != 0 {
		gflags |= net.FlagBroadcast
	}
	if kflags&kernelIffLoopback != 0 {
		gflags |= net.FlagLoopback
	}
	if kflags&kernelIffMulticast != 0 {
		gflags |= net.FlagMulticast
	}
	if kflags&kernelIffRunning != 0 {
		gflags |= net.FlagRunning
	}
	return gflags
}

// UsableInterfaces returns network interfaces that are up, not loopback,
// and support multicast — the ones suitable for LAN mDNS discovery.
//
// Uses a tiered approach to handle restricted environments (Android/Termux):
//  1. net.Interfaces()          — netlink, works on desktop
//  2. ifconfig (exec)           — ioctl-based, works on many Android devices
//  3. InterfaceByName (ioctl)   — hardcoded names as last resort
func UsableInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		slog.Debug("net.Interfaces() failed, trying ifconfig fallback", "err", err)
		all = ifconfigInterfaces()
	}

	if len(all) == 0 {
		slog.Debug("ifconfig returned nothing, trying InterfaceByName fallback")
		all = namedInterfaceFallback()
	}

	var out []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			slog.Debug("skipping (down)", "name", iface.Name)
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			slog.Debug("skipping (loopback)", "name", iface.Name)
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 {
			slog.Debug("skipping (no multicast)", "name", iface.Name, "flags", fmt.Sprintf("0x%x", int(iface.Flags)))
			continue
		}
		out = append(out, iface)
	}
	return out, nil
}

// namedInterfaceFallback tries common Wi-Fi interface names directly via
// net.InterfaceByName(), which uses ioctl (not netlink) and may be allowed
// when net.Interfaces() and ifconfig are both blocked on Android.
func namedInterfaceFallback() []net.Interface {
	var candidates = []string{"wlan0", "wlan1", "eth0", "wifi0"}
	var out []net.Interface
	for _, name := range candidates {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			slog.Debug("InterfaceByName", "name", name, "err", err)
			continue
		}
		slog.Debug("found interface via InterfaceByName",
			"name", iface.Name,
			"flags", fmt.Sprintf("0x%x", int(iface.Flags)),
		)
		out = append(out, *iface)
	}
	return out
}

// ifconfigInterfaces runs "ifconfig" and parses its output to build
// net.Interface values.
func ifconfigInterfaces() []net.Interface {
	// Try without -a first (toybox/busybox on Android may not support it).
	cmd := exec.Command("ifconfig")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("ifconfig failed", "err", err)
		cmd2 := exec.Command("ifconfig", "-a")
		output2, err2 := cmd2.Output()
		if err2 != nil {
			slog.Debug("ifconfig -a also failed", "err", err2)
			return nil
		}
		output = output2
	}

	slog.Debug("ifconfig output", "raw", string(output))
	return parseIfconfig(string(output))
}

// parseIfconfig parses the output of ifconfig into net.Interface values.
// Converts kernel-level flags to Go's net.Flags numbering.
func parseIfconfig(output string) []net.Interface {
	lines := strings.Split(output, "\n")

	var out []net.Interface
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse: "wlan0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500"
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		rest := strings.TrimSpace(line[colonIdx+1:])

		if !strings.HasPrefix(rest, "flags=") {
			continue
		}

		// Extract decimal flags value.
		rest = strings.TrimPrefix(rest, "flags=")
		end := strings.IndexAny(rest, "< ")
		if end == -1 {
			slog.Debug("cannot parse flags for interface", "name", name, "rest", rest)
			continue
		}
		flagsStr := rest[:end]
		kflags, err := strconv.ParseUint(flagsStr, 10, 32)
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

		// Convert kernel flags to Go's net.Flags numbering.
		gflags := kernelFlagsToGo(kflags)

		slog.Debug("found interface",
			"name", name,
			"kernel_flags", fmt.Sprintf("0x%x", kflags),
			"go_flags", fmt.Sprintf("0x%x", int(gflags)),
			"mtu", mtu,
		)

		out = append(out, net.Interface{
			Index: len(out) + 1,
			MTU:   mtu,
			Name:  name,
			Flags: gflags,
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
