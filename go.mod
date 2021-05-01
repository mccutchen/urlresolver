module github.com/mccutchen/urlresolver

go 1.16

require (
	github.com/PuerkitoBio/purell v1.1.1
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/alicebob/miniredis/v2 v2.14.3
	github.com/go-redis/cache/v8 v8.4.0
	github.com/go-redis/redis/v8 v8.8.2
	github.com/honeycombio/beeline-go v1.0.0
	github.com/rs/zerolog v1.21.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210428140749-89ef3d95e781
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/text v0.3.6
)

// https://github.com/honeycombio/beeline-go/pull/216
replace github.com/honeycombio/beeline-go v1.0.0 => github.com/mccutchen/beeline-go v1.0.1
