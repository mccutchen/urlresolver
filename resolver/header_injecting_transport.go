package resolver

import "net/http"

type headerInjectingTransport struct {
	injectHeaders map[string]string
	transport     http.RoundTripper
}

func (t *headerInjectingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	for key, value := range t.injectHeaders {
		r.Header.Set(key, value)
	}
	return t.transport.RoundTrip(r)
}
