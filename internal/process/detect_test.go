package process

import "testing"

func TestDetectURLLaravel(t *testing.T) {
	out := `INFO  Server running on [http://127.0.0.1:8000].`
	got := DetectURL(out)
	if got != "http://127.0.0.1:8000" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectURLVitePrefersLocal(t *testing.T) {
	out := `
  ➜  Local:   http://localhost:5173/
  ➜  Network: http://192.168.1.10:5173/
`
	got := DetectURL(out)
	if got != "http://localhost:5173" && got != "http://localhost:5173/" {
		// normalizeBrowseURL strips trailing slash path "/"
		if got != "http://localhost:5173" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestDetectURLRewritesAllInterfaces(t *testing.T) {
	out := `Listening on http://0.0.0.0:3000`
	got := DetectURL(out)
	if got != "http://0.0.0.0:3000" && got != "http://127.0.0.1:3000" {
		t.Fatalf("got %q", got)
	}
	if got != "http://127.0.0.1:3000" {
		t.Fatalf("want rewritten 127.0.0.1, got %q", got)
	}
	if PortFromURL(got) != 3000 {
		t.Fatalf("port = %d", PortFromURL(got))
	}
}
