package urlresolver

import (
	"encoding/base64"
	"net/url"
	"regexp"
)

// Sailthru click tracking URLs embeds the target URL as a base64-encoded path
// component, e.g.
//
//	https://link.mail.bloombergbusiness.com/click/30059877.127888/aHR0cHM6Ly93d3cuYmxvb21iZXJnLmNvbS9uZXdzL2FydGljbGVzLzIwMjItMTItMjIvZmlyZWQtdHdpdHRlci1tYW5hZ2VyLXN1ZXMtb3Zlci1jYW5jZWxsYXRpb24tb2Ytc3RvY2stb3B0aW9ucz9jbXBpZD1CQkQxMjIyMjJfTU9ORVlTVFVGRiZ1dG1fbWVkaXVtPWVtYWlsJnV0bV9zb3VyY2U9bmV3c2xldHRlciZ1dG1fdGVybT0yMjEyMjImdXRtX2NhbXBhaWduPW1vbmV5c3R1ZmY/5f15b2f8375b191df5003b21B03bb0f6b
//
// embeds
//
//	https://www.bloomberg.com/news/articles/2022-12-22/fired-twitter-manager-sues-over-cancellation-of-stock-options?cmpid=BBD122222_MONEYSTUFF&utm_medium=email&utm_source=newsletter&utm_term=221222&utm_campaign=moneystuff
//
// Regex tester:
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
