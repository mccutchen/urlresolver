package urlresolver

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"

	"github.com/andybalholm/brotli"
)

// Not very sportsmanlike, but basically effective at letting us fetch page
// titles.
var fakeBrowserHeaders = map[string]string{
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	"Accept-Encoding": "gzip, deflate, br", // Ensure decodingBodyReader can handle these encodings
	"Accept-Language": "en-US,en;q=0.5",
	"Referer":         "https://duckduckgo.com/",
	"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:87.0) Gecko/20100101 Firefox/87.0",
}

type fakeBrowserTransport struct {
	transport http.RoundTripper
}

func (t *fakeBrowserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range fakeBrowserHeaders {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	newBody, err := newDecodingBodyReader(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, err
	}
	resp.Body = newBody
	return resp, nil
}

// decodingBodyReader is an io.ReadCloser implementatino that wraps an http
// response body and automatically decodes it based on the Transfer-Encoding
// header value.
type decodingBodyReader struct {
	reader io.Reader
	closer func() error
}

var _ io.ReadCloser = &decodingBodyReader{} // implements io.ReadCloser

func newDecodingBodyReader(r io.ReadCloser, contentEncoding string) (io.ReadCloser, error) {
	switch contentEncoding {
	case "gzip":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("error initializing gzip: %w", err)
		}
		return &decodingBodyReader{reader: gr, closer: closeBoth(gr, r)}, nil
	case "deflate":
		fr := flate.NewReader(r)
		return &decodingBodyReader{reader: fr, closer: closeBoth(fr, r)}, nil
	case "br":
		return &decodingBodyReader{reader: brotli.NewReader(r), closer: r.Close}, nil
	default:
		return r, nil
	}
}

func (r *decodingBodyReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *decodingBodyReader) Close() error {
	return r.closer()
}

// closeBoth returns a new function that ensures that the Close() method of
// both readers are closed. It's used by decodingBodyReader to close both the
// decoding reader (e.g. gzip) and underlying response body reader.
func closeBoth(a, b io.ReadCloser) func() error {
	return func() error {
		err1 := a.Close()
		err2 := b.Close()
		switch {
		case err1 == nil && err2 == nil:
			return nil
		case err1 != nil && err2 == nil:
			return err1
		case err1 == nil && err2 != nil:
			return err2
		default:
			return fmt.Errorf("multiple errors closing reader: %s; %s", err1, err2)
		}
	}
}
