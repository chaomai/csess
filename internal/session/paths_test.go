package session

import "testing"

func TestEncodeCWD(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{"/Users/chaomai/Downloads", "-Users-chaomai-Downloads"},
		{"/", "-"},
		{"/tmp/has space/ok", "-tmp-has space-ok"},
	}
	for _, tt := range tests {
		if got := EncodeCWD(tt.in); got != tt.out {
			t.Errorf("EncodeCWD(%q) = %q; want %q", tt.in, got, tt.out)
		}
	}
}
