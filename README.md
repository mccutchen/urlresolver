# urlresolver

[![Documentation](https://pkg.go.dev/badge/github.com/mccutchen/urlresolver)](https://pkg.go.dev/github.com/mccutchen/urlresolver)
[![Build status](https://github.com/mccutchen/urlresolver/actions/workflows/test.yaml/badge.svg)](https://github.com/mccutchen/urlresolver/actions/workflows/test.yaml)
[![Code coverage](https://codecov.io/gh/mccutchen/urlresolver/branch/main/graph/badge.svg)](https://codecov.io/gh/mccutchen/urlresolver)
[![Go report card](http://goreportcard.com/badge/github.com/mccutchen/urlresolver)](https://goreportcard.com/report/github.com/mccutchen/urlresolver)

A golang package that "resolves" a given URL by issuing a GET request,
following any redirects, canonicalizing the final URL, and attempting to
extract the title from the final response body.

## Methodology

### Resolving

A URL is resolved by issuing a `GET` request and following any redirects until
a non-`30x` response is received.

### Canonicalizing

The final URL is aggressively canonicalized using a combination of
[PuerkitoBio/purell][purell] and some manual heuristics for removing
unnecessary query params (e.g. `utm_*` tracking params), normalizing case (e.g.
`twitter.com/Thresholderbot` and `twitter.com/thresholderbot` are the same).

Canonicalization is optimized for URLs that are shared on social media.

## Security

**TL;DR: Use [`safedialer.Control`][safedialer] in the transport's dialer to
block attempts to resolve URLs pointing at internal, private IP addresses.**

Exposing functionality like this on the internet can be dangerous, because it
could theoretically allow a malicious client to discover information about your
internal network by asking it to resolve URLs whose DNS points at private IP
addresses.

The dangers, along with a golang-specific mitigation, are outlined in Andrew
Ayer's _excellent_ ["Preventing Server Side Request Forgery in Golang"][blog]
blog post.

To mitigate that danger, users are **strongly encouraged** to use
[`safedialer.Control`][safedialer] as the `Control` function in the dialer used
by the transport given to `urlresolver.New`.

See [github.com/mccutchen/urlresolverapi][] for a productionized example, deployed at
https://urlresolver.com.

[Thresholderbot]: https://thresholderbot.com/
[purell]: https://github.com/PuerkitoBio/purell
[blog]: https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golangs
[safedialer]: https://github.com/mccutchen/safedialer
[github.com/mccutchen/urlresolverapi]: https://github.com/mccutchen/urlresolverapi/blob/7e1a30fc0a5f8/cmd/urlresolverapi/main.go#L120-L128
