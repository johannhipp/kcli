package kleinanzeigen

import (
	"errors"
	"net"
	"strings"
)

// IsTransportPreWrite reports whether err indicates the request failed before
// it could reach the network (DNS resolution, connection refused, or connect
// timeout). It walks the wrapped error chain for a *net.OpError whose Op is
// "dial", "proxyconnect", or "connect", or a *net.DNSError. A false result
// means the request may already have been written, so its external outcome
// is unknown and callers must reconcile rather than assume failure.
func IsTransportPreWrite(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch strings.ToLower(opErr.Op) {
		case "dial", "proxyconnect", "connect":
			return true
		}
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}
