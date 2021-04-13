package urlresolver

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/purell"
)

var (
	// Query parameters matching these patterns will ALWAYS be stripped.  The
	// categorized patterns below were largely sourced from this Chrome
	// Extension:
	//
	// https://github.com/newhouse/url-tracking-stripper/blob/dea6c144/README.md#documentation
	excludeParamPattern = listToRegexp(`(?i)^(`, `)$`, []string{
		// Google's Urchin Tracking Module & Google Adwords
		`utm_.+`,
		`gclid`,

		// Adobe Omniture SiteCatalyst
		`icid`,

		// Facebook
		`fbclid`,

		// Hubspot
		`_hsenc`,
		`_hsmi`,

		// Marketo
		`mkt_.+`,

		// MailChimp
		`mc_.+`,

		// Simple Reach
		`sr_.+`,

		// Vero
		`vero_.+`,

		// Unknown
		`nr_email_referer`,
		`ncid`,
		`ref`,

		// Miscellaneous garbage-looking params noticed by @mccutchen while
		// perusing logs
		`_r`,
		`currentPage`,
		`fsrc`,
		`mb?id`,
		`mobile_touch`,
		`ocid`,
		`rss`,
		`s_(sub)?src`,
		`smid`,
		`wpsrc`,
	})

	// Per-domain lists of allowed query parameters
	domainParamAllowlist = map[*regexp.Regexp]*regexp.Regexp{
		regexp.MustCompile(`(?i)(^|\.)youtube\.com$`): regexp.MustCompile(`^(v|p|t|list)$`),
	}

	// All query params will be stripped from these domains, which tend to be
	// content-focused web sites.
	//
	// TODO: this could potentially make us miss roll some urls up together
	// (e.g. in the case of /search?q=foo on a domain), but I think it"s worth
	// it for now.
	stripParamDomainPattern = listToRegexp(`(?i)(^|\.)(`, `)$`, []string{
		`bbc\.co\.uk`,
		`buzzfeed\.com`,
		`deadspin\.com`,
		`economist\.com`,
		`grantland\.com`,
		`huffingtonpost\.com`,
		`instagram\.com`,
		`newyorker\.com`,
		`nymag\.com`,
		`nytimes\.com`,
		`slate\.com`,
		`techcrunch\.com`,
		`theguardian\.com`,
		`theonion\.com`,
		`twitter\.com`,
		`vanityfair\.com`,
		`vulture\.com`,
		`washingtonpost\.com`,
		`wsj\.com`,
	})

	lowercaseDomainPattern = listToRegexp(`(?i)(^|\.)(`, `)$`, []string{
		`instagram\.com`,
		`twitter\.com`,
	})
)

// See https://godoc.org/github.com/PuerkitoBio/purell#NormalizationFlags
const purellNormalizationFlags = (purell.FlagsSafe |
	purell.FlagRemoveDotSegments |
	purell.FlagRemoveDuplicateSlashes |
	purell.FlagDecodeDWORDHost |
	purell.FlagDecodeOctalHost |
	purell.FlagDecodeHexHost |
	purell.FlagRemoveUnnecessaryHostDots |
	purell.FlagRemoveEmptyPortSeparator)

// Canonicalize filters and normalizes a URL
func Canonicalize(u *url.URL) string {
	return Normalize(Clean(u))
}

// Normalize normalizes a URL, ensuring consistent case, encoding, sorting of
// params, etc. See purellNormalizationFlags for the normalization rules we're
// asking the purell library to apply.
func Normalize(u *url.URL) string {
	if lowercaseDomainPattern.MatchString(u.Host) {
		u.Path = strings.ToLower(u.Path)
	}
	return purell.NormalizeURL(u, purellNormalizationFlags)
}

// Clean removes unnecessary query params and fragment identifiers from a URL
func Clean(u *url.URL) *url.URL {
	u.RawQuery = filterParams(u).Encode()
	u.Fragment = ""
	return u
}

func filterParams(u *url.URL) url.Values {
	filtered := url.Values{}
	hostname := u.Hostname()
	for param, values := range u.Query() {
		if shouldExcludeParam(hostname, param) {
			continue
		}
		for _, v := range values {
			filtered.Add(param, v)
		}
	}
	return filtered
}

func shouldExcludeParam(domain string, param string) bool {
	// Is this a param we strip from any domain?
	if excludeParamPattern.MatchString(param) {
		return true
	}

	// Do we strip all params from this domain?
	if stripParamDomainPattern.MatchString(domain) {
		return true
	}

	// Is there a param whitelist for this domain, and is this param on it?
	for domainPattern, whitelistPattern := range domainParamAllowlist {
		if domainPattern.MatchString(domain) {
			return !whitelistPattern.MatchString(param)
		}
	}

	// Default to include params
	return false
}

func listToRegexp(prefix string, suffix string, patterns []string) *regexp.Regexp {
	combinedPattern := fmt.Sprintf("%s%s%s", prefix, strings.Join(patterns, "|"), suffix)
	return regexp.MustCompile(combinedPattern)
}