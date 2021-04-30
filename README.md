# urlresolver

A golang package and associated HTTP service that "resolves" a URL into its
canonical form and, if it points to an HTML document, attempts to fetch its
title.

It is used by [Thresholderbot][] to resolve URLs found in tweets, which tend to
be wrapped in one or more URL shorteners (t.co, bit.ly, etc).

## Resolving

A URL is resolved by issuing a `GET` request and following any redirects until
a non-`30x` response is received.

These requests are made by a customized transport that sends fake browser
headers and properly handles cookies between redirects, in an underhanded
attempt to maximize the possibility of fetching an accurate title.

## Canonicalizing

The final URL is aggressively canonicalized using a combination of
[PuerkitoBio/purell][purell] and some manual heuristics for removing
unnecessary query params (e.g. `utm_*` tracking params), normalizing case (e.g.
`twitter.com/Thresholderbot` and `twitter.com/thresholderbot` are the same).

The canonicalization is optimized for URLs that are shared on social media.

## Security

**TL;DR: Use `safedialer.Control` in the transport's dialer to block attempts
to resolve URLs pointing at internal, private IP addresses.**

Exposing functionality like this on the internet can be dangerous, because it
could theoretically allow a malicious client to discover information about your
internal network by asking it to resolve URLs pointing at private IP addresses.

The dangers, along with a golang-specific mitigation, are outlined in Andrew
Ayer's _excellent_ ["Preventing Server Side Request Forgery in Golang"][blog]
blog post.

To mitigate that danger, users are **strongly encouraged** to use
`safedialer.Control` as the `Control` function in the dialer used by the
transport given to `urlresolver.New`.

[Thresholderbot]: https://thresholderbot.com/
[purell]: https://github.com/PuerkitoBio/purell
[blog]: https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golangs
