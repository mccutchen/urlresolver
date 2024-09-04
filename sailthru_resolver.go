package urlresolver

import (
	"encoding/base64"
	"net/url"
	"regexp"
)

// https://regex101.com/r/fVcveA/1
var sailthruRegex = regexp.MustCompile(`(?i)^https?://[^/]+/click/\d+\.\d+/([A-Za-z0-9=]+)/.+`)

func matchSailthruURL(s string) (string, bool) {
	if matches := sailthruRegex.FindStringSubmatch(s); matches != nil {
		return matches[1], true
	}
	return "", false
}

func decodeSailthruURL(encodedURL string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(encodedURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(string(b))
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
