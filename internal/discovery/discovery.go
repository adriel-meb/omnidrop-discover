// Package discovery implements mDNS-based LAN peer discovery using Zeroconf
// (Bonjour/Avahi). It provides both service advertising (serve mode) and
// service browsing (scan mode), plus a direct TCP banner protocol for
// out-of-band peer probing.
package discovery

// Service constants shared across the package.
const (
	// serviceType is the DNS-SD service type used for omnidrop discovery.
	// Other omnidrop instances browse for this type on the LAN.
	serviceType = "_omnidrop._tcp"

	// serviceDomain is the mDNS domain. "local." is the standard link-local
	// multicast domain defined by RFC 6762.
	serviceDomain = "local."

	// appVersion is the version string advertised in TXT records.
	appVersion = "0.1.0"
)
