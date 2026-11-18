package git

import "testing"

func TestNoPR(t *testing.T) {
	if !noPR(`no pull requests found for branch "dev"`) {
		t.Error("no-PR message not recognised")
	}
	for _, s := range []string{"error connecting to api.github.com", "Post \"https://api.github.com/graphql\": net/http: TLS handshake timeout", ""} {
		if noPR(s) {
			t.Errorf("%q taken for no PR", s)
		}
	}
}
