package urlresolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSailthruResolver(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		given          string
		wantDecodedURL string
		wantDecodeErr  bool
	}{
		"live valid example": {
			given:          "https://link.mail.bloombergbusiness.com/click/30059877.127888/aHR0cHM6Ly93d3cuYmxvb21iZXJnLmNvbS9uZXdzL2FydGljbGVzLzIwMjItMTItMjIvZmlyZWQtdHdpdHRlci1tYW5hZ2VyLXN1ZXMtb3Zlci1jYW5jZWxsYXRpb24tb2Ytc3RvY2stb3B0aW9ucz9jbXBpZD1CQkQxMjIyMjJfTU9ORVlTVFVGRiZ1dG1fbWVkaXVtPWVtYWlsJnV0bV9zb3VyY2U9bmV3c2xldHRlciZ1dG1fdGVybT0yMjEyMjImdXRtX2NhbXBhaWduPW1vbmV5c3R1ZmY/5f15b2f8375b191df5003b21B03bb0f6b",
			wantDecodedURL: "https://www.bloomberg.com/news/articles/2022-12-22/fired-twitter-manager-sues-over-cancellation-of-stock-options?cmpid=BBD122222_MONEYSTUFF&utm_medium=email&utm_source=newsletter&utm_term=221222&utm_campaign=moneystuff",
		},
		"invalid base64 data": {
			given:         "https://example.com/click/1234567890.123456/0/0",
			wantDecodeErr: true,
		},
		"invalid encoded URL": {
			given:         "https://example.com/click/1234567890.123456/aHR0cDovLyUlCg/0",
			wantDecodeErr: true,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.given, func(t *testing.T) {
			t.Parallel()
			encodedURL, ok := matchSailthruURL(tc.given)
			assert.True(t, ok)
			decodedURL, err := decodeSailthruURL(encodedURL)
			if tc.wantDecodeErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantDecodedURL, decodedURL)
		})
	}
}
