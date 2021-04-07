// Package safedialer provides a net.Dialer that rejects attempts to dial
// internal/private networks.s
//
// This code was lightly adapted from Andrew Ayer's excellent "Preventing
// Server Side Request Forgery in Golang" blog post:
// https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golangs
package safedialer

/*
 * Written in 2019 by Andrew Ayer
 *
 * To the extent possible under law, the author(s) have dedicated all
 * copyright and related and neighboring rights to this software to the
 * public domain worldwide. This software is distributed without any
 * warranty.
 *
 * You should have received a copy of the CC0 Public
 * Domain Dedication along with this software. If not, see
 * <https://creativecommons.org/publicdomain/zero/1.0/>.
 */

import (
	"fmt"
	"net"
	"syscall"
)

// New creates a new http.Transport configured to reject attempts to dial
// internal/private network addresses.
func New(d net.Dialer) *net.Dialer {
	d.Control = safeSocketControl
	return &d
}

func safeSocketControl(network string, address string, conn syscall.RawConn) error {
	if !(network == "tcp4" || network == "tcp6") {
		return fmt.Errorf("%s is not a safe network type", network)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s is not a valid host/port pair: %s", address, err)
	}

	if !(port == "80" || port == "443") {
		return fmt.Errorf("%s is not a safe port number", port)
	}

	ipaddress := net.ParseIP(host)
	if ipaddress == nil {
		return fmt.Errorf("%s is not a valid IP address", host)
	}

	if !isPublicIPAddress(ipaddress) {
		return fmt.Errorf("%s is not a public IP address", ipaddress)
	}

	return nil
}
