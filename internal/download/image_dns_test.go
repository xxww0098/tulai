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

	"golang.org/x/net/dns/dnsmessage"
)

func TestImageFakeIPResolutionRequiresOnlyFakeAddresses(t *testing.T) {
	fake := netip.MustParseAddr("198.18.1.18")
	mappedFake := netip.MustParseAddr("::ffff:198.19.0.1")
	public := netip.MustParseAddr("93.184.216.34")
	local := netip.MustParseAddr("127.0.0.1")
	systemErr := errors.New("system DNS failed")
	for _, test := range []struct {
		host      string
		ips       []netip.Addr
		lookupErr error
		fallback  string
	}{
		{host: "img.kwcdn.com", ips: []netip.Addr{fake}, fallback: "img.kwcdn.com"},
		{host: "I.Etsystatic.COM.", ips: []netip.Addr{fake, mappedFake}, fallback: "i.etsystatic.com"},
		{host: "d2j6dbq0eux0bg.cloudfront.net", ips: []netip.Addr{fake}, fallback: "d2j6dbq0eux0bg.cloudfront.net"},
		{host: "例子.cn", ips: []netip.Addr{fake}, fallback: "xn--fsqu00a.cn"},
		{host: "router", ips: []netip.Addr{fake}},
		{host: "router.local", ips: []netip.Addr{fake}},
		{host: "router.home.arpa", ips: []netip.Addr{fake}},
		{host: "i.pinimg.com.evil.test", ips: []netip.Addr{fake}},
		{host: "image..example.com", ips: []netip.Addr{fake}},
		{host: "198.18.1.18", ips: []netip.Addr{fake}},
		{host: "i.pinimg.com", ips: []netip.Addr{local}},
		{host: "i.pinimg.com", ips: []netip.Addr{fake, local}},
		{host: "i.pinimg.com", ips: []netip.Addr{fake, public}},
		{host: "i.pinimg.com", ips: []netip.Addr{public}},
		{host: "i.pinimg.com"},
		{host: "i.pinimg.com", lookupErr: systemErr},
	} {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) { return test.ips, test.lookupErr }
		var queried string
		resolve := func(_ context.Context, name string) ([]netip.Addr, error) {
			queried = name
			return []netip.Addr{public}, nil
		}
		ips, err := lookupImageIPsWith(context.Background(), "ip", test.host, lookup, resolve)
		if queried != test.fallback || !errors.Is(err, test.lookupErr) {
			t.Fatalf("%s with system answers %v queried %q and returned %v, %v", test.host, test.ips, queried, ips, err)
		}
		if queried != "" && (len(ips) != 1 || ips[0] != public) {
			t.Fatalf("%s fallback returned %v", test.host, ips)
		}
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { return []netip.Addr{fake}, nil }
	for _, resolveErr := range []error{nil, errors.New("unavailable"), context.Canceled} {
		resolve := func(context.Context, string) ([]netip.Addr, error) { return nil, resolveErr }
		ips, err := lookupImageIPsWith(context.Background(), "ip", "i.pinimg.com", lookup, resolve)
		if len(ips) != 0 || err == nil || (resolveErr != nil && !errors.Is(err, resolveErr)) {
			t.Fatalf("failed fallback must remain blocked: %v %v", ips, err)
		}
	}
}

func TestImageDNSRejectsPrivateAndMismatchedAnswers(t *testing.T) {
	const valid = `{"Status":0,"Question":[{"name":"i.pinimg.com.","type":1}],"Answer":[{"type":5,"data":"alias.example.net."},{"type":1,"data":"127.0.0.1"},{"type":1,"data":"198.18.1.18"},{"type":1,"data":"93.184.216.34"}]}`
	body := valid
	recordType := "1"
	client := &http.Client{Transport: temuRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cloudflare-dns.com" || req.URL.Query().Get("name") != "i.pinimg.com" ||
			req.URL.Query().Get("type") != recordType || req.Header.Get("Accept") != "application/dns-json" {
			t.Fatalf("unexpected DNS request: %v", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	ips, err := queryImageDNS(context.Background(), client, "i.pinimg.com", dnsmessage.TypeA)
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("93.184.216.34") {
		t.Fatalf("DNS answers=%v err=%v", ips, err)
	}
	for _, invalid := range []string{
		strings.Replace(valid, "i.pinimg.com.", "other.example.", 1),
		strings.Replace(valid, `"type":1`, `"type":28`, 1),
		strings.Replace(valid, `"Status":0`, `"Status":3`, 1),
		strings.Replace(valid, `"Status":0`, `"TC":true,"Status":0`, 1),
		strings.Replace(valid, `"Status":0,`, "", 1),
	} {
		body = invalid
		if _, err := queryImageDNS(context.Background(), client, "i.pinimg.com", dnsmessage.TypeA); err == nil {
			t.Fatalf("accepted an invalid DNS response: %s", body)
		}
	}
	body = strings.Replace(valid, `"93.184.216.34"`, `"10.0.0.1"`, 1)
	if ips, err := queryImageDNS(context.Background(), client, "i.pinimg.com", dnsmessage.TypeA); err != nil || len(ips) != 0 {
		t.Fatalf("non-public DNS answers=%v err=%v", ips, err)
	}
	recordType = "28"
	body = `{"Status":0,"Question":[{"name":"i.pinimg.com","type":28}],"Answer":[{"type":28,"data":"::1"},{"type":28,"data":"::ffff:93.184.216.34"},{"type":28,"data":"2001:4860:4860::8888"}]}`
	ips, err = queryImageDNS(context.Background(), client, "i.pinimg.com", dnsmessage.TypeAAAA)
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("2001:4860:4860::8888") {
		t.Fatalf("IPv6 DNS answers=%v err=%v", ips, err)
	}
}

func TestImageFakeIPFallbackPinsPublicAddress(t *testing.T) {
	fake := netip.MustParseAddr("198.18.1.18")
	private := netip.MustParseAddr("127.0.0.1")
	public := netip.MustParseAddr("93.184.216.34")
	answers := []netip.Addr{private, public}
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		return lookupImageIPsWith(ctx, network, host,
			func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{fake}, nil
			},
			func(context.Context, string) ([]netip.Addr, error) {
				return answers, nil
			})
	}
	var dialed []string
	dial := func(_ context.Context, _, target string) (net.Conn, error) {
		dialed = append(dialed, target)
		client, peer := net.Pipe()
		_ = peer.Close()
		return client, nil
	}
	ctx := context.WithValue(context.Background(), safeDownloadPolicyKey{}, safeDownloadPolicy{allowedLocal: fake})
	conn, err := safeDownloadDialWith(ctx, "tcp", "i.etsystatic.com:443", lookup, dial)
	if err != nil || len(dialed) != 1 || dialed[0] != "93.184.216.34:443" {
		t.Fatalf("fallback was not pinned to its public address: dialed=%v err=%v", dialed, err)
	}
	_ = conn.Close()
	dialed, answers = nil, []netip.Addr{private, fake}
	if _, err := safeDownloadDialWith(ctx, "tcp", "i.etsystatic.com:443", lookup, dial); err == nil || len(dialed) != 0 {
		t.Fatalf("non-public fallback reached dial: dialed=%v err=%v", dialed, err)
	}
	if _, err := safeDownloadDialWith(ctx, "tcp", "198.18.1.18:443", lookup, dial); err == nil || len(dialed) != 0 {
		t.Fatalf("explicit fake IP reached dial: dialed=%v err=%v", dialed, err)
	}
}
