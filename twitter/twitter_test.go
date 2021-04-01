package twitter

import (
	"strconv"
	"testing"
)

func TestMatchTweetURL(t *testing.T) {
	testCases := []struct {
		given   string
		wantURL string
		wantOK  bool
	}{
		// basic tweet urls
		{"https://twitter.com/thresholderbot/status/1341197329550995456", "https://twitter.com/thresholderbot/status/1341197329550995456", true},

		// mobile links okay
		{"https://mobile.twitter.com/thresholderbot/status/1341197329550995456", "https://mobile.twitter.com/thresholderbot/status/1341197329550995456", true},

		// media links truncated to parent tweet
		{"https://twitter.com/thresholderbot/status/1341197329550995456/photo/1", "https://twitter.com/thresholderbot/status/1341197329550995456", true},

		// case insensitive
		{"https://TWITTER.com/McCutchen/status/1255896435540676610", "https://TWITTER.com/McCutchen/status/1255896435540676610", true},

		// query parameters ignored
		{"https://twitter.com/thresholderbot/status/1341197329550995456?utm_whatever=foo", "https://twitter.com/thresholderbot/status/1341197329550995456", true},
	}
	for _, tc := range testCases {
		t.Run(tc.given, func(t *testing.T) {
			url, ok := MatchTweetURL(tc.given)
			if url != tc.wantURL {
				t.Errorf("expected url == %q, got %q", tc.wantURL, url)
			}
			if ok != tc.wantOK {
				t.Errorf("expected ok == %v, got %v", tc.wantOK, ok)
			}
		})
	}
}

func TestExtractTweetText(t *testing.T) {
	testCases := []struct {
		given string
		want  string
	}{
		{
			// https://publish.twitter.com/oembed?url=https://twitter.com/NekiasNBA/status/1377329865133801484
			given: `<blockquote class=\"twitter-tweet\">
    <p lang=\"en\" dir=\"ltr\">🚨NEW WORDS ALERT🚨<br><br>Introducing <a
            href=\"https://twitter.com/hashtag/RoamingTheBaseline?src=hash&amp;ref_src=twsrc%5Etfw\">#RoamingTheBaseline</a>,
        a weekly notebook where I write about whatever catches my eye.<br><br>We’re talking Devonte’ pull-ups, an ode to
        Christyn Williams, some empty corner goodness and more!<a
            href=\"https://t.co/MykXNJkycw\">https://t.co/MykXNJkycw</a></p>&mdash; Nekias Duncan (@NekiasNBA) <a
        href=\"https://twitter.com/NekiasNBA/status/1377329865133801484?ref_src=twsrc%5Etfw\">March 31, 2021</a>
</blockquote>\n
<script async src=\"https://platform.twitter.com/widgets.js\" charset=\"utf-8\"></script>\n`,
			want: "🚨NEW WORDS ALERT🚨 Introducing #RoamingTheBaseline, a weekly notebook where I write about whatever catches my eye. We’re talking Devonte’ pull-ups, an ode to Christyn Williams, some empty corner goodness and more! https://t.co/MykXNJkycw",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			got := extractTweetText(tc.given)
			if got != tc.want {
				t.Errorf("incorrect extraction results:\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}

func TestFetch(t *testing.T) {

}
