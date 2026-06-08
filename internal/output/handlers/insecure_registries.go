package handlers

import (
	"net"
	"strings"
)

// matchesInsecureRegistry reports whether ref's registry host is enumerated
// in the user's oci.insecure_registries list. Host-only entries match any
// port ("localhost" → "localhost:5000"); host:port entries match exactly.
// IPv6 addresses may be bare (`::1`) or bracketed (`[::1]:5000`).
func matchesInsecureRegistry(ref string, insecureRegistries []string) bool {
	host := registryHost(ref)
	if host == "" || len(insecureRegistries) == 0 {
		return false
	}
	refHost, refPort := splitHostPort(host)
	for _, entry := range insecureRegistries {
		entryHost, entryPort := splitHostPort(entry)
		if entryHost != refHost {
			continue
		}
		if entryPort == "" || entryPort == refPort {
			return true
		}
	}
	return false
}

// registryHost returns the registry portion of an OCI reference (everything
// before the first "/"). Empty for malformed refs.
func registryHost(ref string) string {
	if i := strings.IndexByte(ref, '/'); i > 0 {
		return ref[:i]
	}
	return ""
}

// splitHostPort tolerates entries without a port (the host-only case the
// matcher supports) and bracketed IPv6 literals — both shapes that
// net.SplitHostPort itself rejects.
func splitHostPort(s string) (host, port string) {
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.Trim(s, "[]"), ""
}
