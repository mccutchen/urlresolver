package twitter

import (
	"net"
	"net/http"
	"time"
)

const (
	// dialer
	dialTimeout = 2 * time.Second
	keepAlive   = 30 * time.Second

	// transport
	expectContinueTimeout = 1 * time.Second
	idleConnTimeout       = 90 * time.Second
	maxIdleConns          = 100
	maxIdleConnsPerHost   = 100
	tlsHandshakeTimeout   = 10 * time.Second

	// http
	requestTimeout = 5 * time.Second
)

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: keepAlive,
			}).DialContext,
			ExpectContinueTimeout: expectContinueTimeout,
			IdleConnTimeout:       idleConnTimeout,
			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
		},
		Timeout: requestTimeout,
	}
}
