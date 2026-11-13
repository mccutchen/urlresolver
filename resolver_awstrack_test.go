package urlresolver

import (
	"errors"
	"testing"

	"github.com/mccutchen/urlresolver/internal/testing/assert"
)

func TestAWSTrackResolver(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		given          string
		wantMatch      bool
		wantDecodedURL string
		wantDecodeErr  error
	}{
		"live valid example": {
			given:          "https://m9tcjpk8.r.us-east-1.awstrack.me/L0/https:%2F%2Fmyaccount.deadriver.com/1/010001929f5ba13f-db88fc0d-677b-4d9a-9505-888afcef5846-000000/FDZcKs7MnWlmm4PnchTvPdaNKBs=396",
			wantMatch:      true,
			wantDecodedURL: "https://myaccount.deadriver.com",
		},
		"destination with path and query": {
			given:          "https://abcd1234.r.us-west-2.awstrack.me/L0/https:%2F%2Fexample.com%2Fa%2Fb%3Fx=1%26y=2/1/0000/sig=1",
			wantMatch:      true,
			wantDecodedURL: "https://example.com/a/b?x=1&y=2",
		},
		"invalid escape": {
			given:         "https://abcd1234.r.us-west-2.awstrack.me/L0/https:%2F%2Fexample.com%ZZ/1/0000/sig=1",
			wantMatch:     true,
			wantDecodeErr: errors.New("invalid URL escape \"%ZZ\""),
		},
		"not an absolute http URL": {
			given:         "https://abcd1234.r.us-west-2.awstrack.me/L0/foo/1/0000/sig=1",
			wantMatch:     true,
			wantDecodeErr: errors.New("invalid destination URL: foo"),
		},
		"unescaped destination is rejected": {
			given:         "https://abcd1234.r.us-west-2.awstrack.me/L0/https://example.com/1/0000/sig=1",
			wantMatch:     true,
			wantDecodeErr: errors.New("invalid destination URL: https:"),
		},
		"other domain": {
			given:     "https://example.com/L0/https:%2F%2Fexample.com/1/0000/sig=1",
			wantMatch: false,
		},
	}
	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encodedURL, ok := matchAWSTrackURL(tc.given)
			assert.Equal(t, ok, tc.wantMatch)
			if !ok {
				return
			}
			decodedURL, err := decodeAWSTrackURL(encodedURL)
			if tc.wantDecodeErr != nil {
				assert.Error(t, err, tc.wantDecodeErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, decodedURL, tc.wantDecodedURL)
		})
	}
}
