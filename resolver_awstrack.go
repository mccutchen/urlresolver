package urlresolver

import (
	"errors"
	"net/url"
	"regexp"
)

// AWS SES click tracking URLs inline the URL-encoded destination URL as a
// URL-encoded path segment, e.g.
//
//	https://4mztdshi.r.us-east-1.awstrack.me/L0/https:%2F%2Feshares.app.link%2Fabcdefg/1/010001a174098991-dab64257-46e6-40c4-9264-a4ea06d51194-000000/knBGaVYCYL4LagcIYK4VhQUBpGQ=473
//
// embeds
//
//	https://eshares.app.link/abcdefg
//
// Regex tester:
// https://regex101.com/r/LJQ6Uu/1
var awsTrackRegex = regexp.MustCompile(`(?i)^https?://[^/]+\.awstrack\.me/L\d+/([^/]+)/.+`)

func matchAWSTrackURL(s string) (string, bool) {
	if matches := awsTrackRegex.FindStringSubmatch(s); matches != nil {
		return matches[1], true
	}
	return "", false
}

func decodeAWSTrackURL(encodedURL string) (string, error) {
	decoded, err := url.PathUnescape(encodedURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(decoded)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("invalid destination URL: " + decoded)
	}
	return u.String(), nil
}
