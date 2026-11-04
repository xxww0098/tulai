package download

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestTemuImageFakeIPResolutionIsScoped(t *testing.T) {
	fake := netip.MustParseAddr("198.18.1.18")
	public := netip.MustParseAddr("93.184.216.34")
	answers := []netip.Addr{fake}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { return answers, nil }
	calls := 0
	resolve := func(_ context.Context, host string) ([]netip.Addr, error) {
		calls++
		if host != "img.kwcdn.com" {
			t.Fatalf("unexpected fallback host %q", host)
		}
		return []netip.Addr{public}, nil
	}
	ips, err := lookupTemuImageIPsWith(context.Background(), "ip", "img.kwcdn.com", lookup, resolve)
	if err != nil || len(ips) != 1 || ips[0] != public || calls != 1 {
		t.Fatalf("fake IP fallback=%v calls=%d err=%v", ips, calls, err)
	}
	for _, tc := range []struct {
		host string
		ips  []netip.Addr
	}{
		{"evil.test", []netip.Addr{fake}},
		{"img.kwcdn.com.evil.test", []netip.Addr{fake}},
		{"img.kwcdn.com", []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{"img.kwcdn.com", []netip.Addr{fake, netip.MustParseAddr("127.0.0.1")}},
		{"img.kwcdn.com", []netip.Addr{public}},
	} {
		answers = tc.ips
		if _, err := lookupTemuImageIPsWith(context.Background(), "ip", tc.host, lookup, resolve); err != nil || calls != 1 {
			t.Fatalf("unexpected fallback host=%s ips=%v calls=%d err=%v", tc.host, tc.ips, calls, err)
		}
	}
	answers = []netip.Addr{fake}
	failed := func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("unavailable") }
	ips, err = lookupTemuImageIPsWith(context.Background(), "ip", "img.kwcdn.com", lookup, failed)
	if err != nil || len(ips) != 1 || ips[0] != fake || isPublicDownloadIP(ips[0]) {
		t.Fatalf("fallback failure must remain blocked: %v %v", ips, err)
	}
}

func TestTemuImageDNSRejectsPrivateAndMismatchedAnswers(t *testing.T) {
	body := `{"Status":0,"Question":[{"name":"img.kwcdn.com.","type":1}],"Answer":[{"type":1,"data":"127.0.0.1"},{"type":1,"data":"198.18.1.18"},{"type":1,"data":"93.184.216.34"}]}`
	client := &http.Client{Transport: temuRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cloudflare-dns.com" || req.URL.Query().Get("name") != "img.kwcdn.com" || req.Header.Get("Accept") != "application/dns-json" {
			t.Fatalf("unexpected DNS request: %v", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	ips, err := queryTemuImageDNS(context.Background(), client, "img.kwcdn.com")
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("93.184.216.34") {
		t.Fatalf("DNS answers=%v err=%v", ips, err)
	}
	body = strings.Replace(body, "img.kwcdn.com.", "other.example.", 1)
	if _, err := queryTemuImageDNS(context.Background(), client, "img.kwcdn.com"); err == nil {
		t.Fatal("accepted a mismatched DNS question")
	}
	if _, err := queryTemuImageDNS(context.Background(), client, "img.kwcdn.com.evil.test"); err == nil {
		t.Fatal("accepted a non-Temu hostname")
	}
}

func TestTemuImageFakeIPFallbackCannotDialPrivate(t *testing.T) {
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		return lookupTemuImageIPsWith(ctx, network, host,
			func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("198.18.1.18")}, nil
			},
			func(context.Context, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			})
	}
	dialed := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected dial")
	}
	if _, err := safeDownloadDialWith(context.Background(), "tcp", "img.kwcdn.com:443", lookup, dial); err == nil || dialed {
		t.Fatalf("private fallback reached dial: called=%v err=%v", dialed, err)
	}
}
