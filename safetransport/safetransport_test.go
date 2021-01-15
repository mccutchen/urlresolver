package safetransport

import (
	"errors"
	"testing"
)

func TestSafeSocketControl(t *testing.T) {
	testCases := []struct {
		net     string
		addr    string
		wantErr error
	}{
		{
			wantErr: nil,
			net:     "tcp4",
			addr:    "185.199.111.153:443",
		},
		{
			wantErr: nil,
			net:     "tcp4",
			addr:    "185.199.111.153:80",
		},
		{
			wantErr: errors.New("udp is not a safe network type"),
			net:     "udp",
		},
		{
			wantErr: errors.New("185.199.111.153 is not a valid host/port pair: address 185.199.111.153: missing port in address"),
			net:     "tcp4",
			addr:    "185.199.111.153",
		},
		{
			wantErr: errors.New("53 is not a safe port number"),
			net:     "tcp4",
			addr:    "185.199.111.153:53",
		},
		{
			wantErr: errors.New("10.51.50.10 is not a public IP address"),
			net:     "tcp4",
			addr:    "10.51.50.10:80",
		},
		{
			wantErr: errors.New("zzz is not a valid IP address"),
			net:     "tcp6",
			addr:    "zzz:443",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.net+"/"+tc.addr, func(t *testing.T) {
			err := safeSocketControl(tc.net, tc.addr, nil)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %s", err)
					return
				}
			} else {
				if err == nil {
					t.Errorf("got err %q, expected nil", tc.wantErr)
					return
				}
				if !(err == tc.wantErr || errors.Is(err, tc.wantErr) || err.Error() == tc.wantErr.Error()) {
					t.Errorf("got err %q, expected %q", err, tc.wantErr)
					return
				}
			}
		})
	}
}
