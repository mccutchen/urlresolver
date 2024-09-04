package urlresolver

import (
	"net/url"
	"testing"
)

type testCase struct {
	name     string
	given    string
	expected string
}

func TestCanonicalize(t *testing.T) {
	t.Parallel()

	testCases := []testCase{
		// Normalization
		{
			name:     "escaping spaces in various places",
			given:    "http://example.com/my path?my param=my value",
			expected: "http://example.com/my%20path?my+param=my+value",
		},
		{
			name:     "spaces in query param keys are escaped",
			given:    "http://example.com/foo?my favorite pet=dog",
			expected: "http://example.com/foo?my+favorite+pet=dog",
		},
		{
			name:     "query params are sorted",
			given:    "http://example.com/foo?z=z&a=a&y=y&b=b",
			expected: "http://example.com/foo?a=a&b=b&y=y&z=z",
		},
		{
			name:     "query params are sorted, duplicate params maintain order",
			given:    "http://example.com/foo?z=z&a=a2&y=y&a=a1",
			expected: "http://example.com/foo?a=a2&a=a1&y=y&z=z",
		},

		// Differences from python canonicalization
		{
			name:     "non-ascii characters are escaped",
			given:    "http://70sscifiart.tumblr.com/post/179321374440/andré-franquin",
			expected: "http://70sscifiart.tumblr.com/post/179321374440/andr%C3%A9-franquin",
		},
		{
			name:     "non-ascii characters are escaped part II",
			given:    "http://arabic.sport360.com/article/كرة-انجليزية/liverpool/707640/كلوب-يتلقى-أخباراً-سارة-بشأن-محمد-صلاح-قبل-مباراة-ليفربول-القادمة/",
			expected: "http://arabic.sport360.com/article/%D9%83%D8%B1%D8%A9-%D8%A7%D9%86%D8%AC%D9%84%D9%8A%D8%B2%D9%8A%D8%A9/liverpool/707640/%D9%83%D9%84%D9%88%D8%A8-%D9%8A%D8%AA%D9%84%D9%82%D9%89-%D8%A3%D8%AE%D8%A8%D8%A7%D8%B1%D8%A7%D9%8B-%D8%B3%D8%A7%D8%B1%D8%A9-%D8%A8%D8%B4%D8%A3%D9%86-%D9%85%D8%AD%D9%85%D8%AF-%D8%B5%D9%84%D8%A7%D8%AD-%D9%82%D8%A8%D9%84-%D9%85%D8%A8%D8%A7%D8%B1%D8%A7%D8%A9-%D9%84%D9%8A%D9%81%D8%B1%D8%A8%D9%88%D9%84-%D8%A7%D9%84%D9%82%D8%A7%D8%AF%D9%85%D8%A9/",
		},

		// Domain specific params
		{
			name:     "all youtube param filtering",
			given:    "https://www.youtube.com/watch?v=zv0N9-rl91I&p=foo&list=bar&t=1m3s&junk=1&morejunk=2",
			expected: "https://www.youtube.com/watch?list=bar&p=foo&t=1m3s&v=zv0N9-rl91I",
		},
		{
			name:     "youtube individual param filtering",
			given:    "https://www.youtube.com/watch?v=abcd1234&foo=bar",
			expected: "https://www.youtube.com/watch?v=abcd1234",
		},
		{
			name:     "youtube strict param match",
			given:    "https://www.youtube.com/watch?v=abcd1234&vv=XXX",
			expected: "https://www.youtube.com/watch?v=abcd1234",
		},
		{
			name:     "twitter search query",
			given:    "https://twitter.com/search?q=query&foo=bar",
			expected: "https://twitter.com/search?q=query",
		},

		// Domains for from which all query params are removed
		{
			name:     "all params are removed from domain with www",
			given:    "http://www.BuzzFeed.COM/foo?a=1&b=2&c=3",
			expected: "http://www.buzzfeed.com/foo",
		},
		{
			name:     "all params are removed from domain without www",
			given:    "http://buzzfeed.com/foo?a=1&b=2&c=3",
			expected: "http://buzzfeed.com/foo",
		},
		{
			name:     "all params are removed from domain only if exact match",
			given:    "http://mybuzzfeed.com/foo?a=1&b=2&c=3",
			expected: "http://mybuzzfeed.com/foo?a=1&b=2&c=3",
		},

		// Params stripped from any domain
		{
			name:     "tracking params are stripped",
			given:    "https://example.com/foo?bar=baz&utm_source=src",
			expected: "https://example.com/foo?bar=baz",
		},
		{
			name:     "tracking params are stripped from domains with whitelists",
			given:    "https://www.youtube.com/watch?v=abcd1234&fbcid=789",
			expected: "https://www.youtube.com/watch?v=abcd1234",
		},
		{
			name:     "tracking params are stripped from domains with param whitelists",
			given:    "https://www.youtube.com/watch?v=abcd1234&fbcid=789",
			expected: "https://www.youtube.com/watch?v=abcd1234",
		},

		// Domains for which URLs are lowercased
		{
			name:     "twitter lowercase",
			given:    "https://Twitter.COM/McCutchen/status/12345",
			expected: "https://twitter.com/mccutchen/status/12345",
		},
		{
			name:     "instagram lowercase",
			given:    "https://instagram.com/McCutchen",
			expected: "https://instagram.com/mccutchen",
		},

		// Misc live examples
		{
			name:     "misc other ad trackers",
			given:    "https://cozyhoome.com/products/ultimate-battle-blaster?utm_source=facebook&utm_medium=Instagram_Feed&utm_content=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29策略1-AP-AA&utm_campaign=AdTestingCompaign&ad_name=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29策略1-AP-AA&adset_name=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29-AP-AA+-+广告副本&omega_utm_source=facebook&omega_utm_medium=Instagram_Feed&omega_utm_content=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29策略1-AP-AA&omega_utm_campaign=AdTestingCompaign&omega_ad_name=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29策略1-AP-AA&omega_adset_name=ultimate-battle-blaster%282014.06.21-电动水枪-原素材-916.mp4%29美国%2806.29%29-AP-AA+-+广告副本&fbclid=PAZXh0bgNhZW0BMAABphA7q6UnxbUJXjZTj2BQEJIoQcLnESUDHN-7xKqd_GY7azNECaFfzMlgcQ_aem_JUubgFX1pzpCn7zlN9ZMFw&campaign_id=120212446259280673&ad_id=120212446259320673&variant=50129381458194",
			expected: "https://cozyhoome.com/products/ultimate-battle-blaster",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.given)
			if err != nil {
				t.Errorf("error parsing %s: %s", tc.given, err)
			}

			result := Canonicalize(u)
			if result != tc.expected {
				t.Errorf("\nGot:  %s\nWant: %s", result, tc.expected)
			}
		})
	}
}
