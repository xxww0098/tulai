package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var imageFakeIPRange = netip.MustParsePrefix("198.18.0.0/15")

var imageDNSClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", "1.1.1.1:443")
		},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 5 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func lookupImageIPs(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return lookupImageIPsWith(ctx, network, host, net.DefaultResolver.LookupNetIP, func(ctx context.Context, host string) ([]netip.Addr, error) {
		ips, err := queryImageDNS(ctx, imageDNSClient, host, dnsmessage.TypeA)
		if err != nil || len(ips) != 0 {
			return ips, err
		}
		return queryImageDNS(ctx, imageDNSClient, host, dnsmessage.TypeAAAA)
	})
}

func imageDNSHostname(host string) string {
	name, err := idna.Lookup.ToASCII(host)
	name = strings.TrimSuffix(name, ".")
	dot := strings.LastIndexByte(name, '.')
	if err != nil || dot < 1 || len(name) > 253 {
		return ""
	}
	topLevel := name[dot+1:]
	if _, icann := publicsuffix.PublicSuffix(topLevel); !icann || topLevel == "arpa" {
		return ""
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return ""
		}
	}
	return name
}

func lookupImageIPsWith(ctx context.Context, network, host string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	resolve func(context.Context, string) ([]netip.Addr, error),
) ([]netip.Addr, error) {
	ips, err := lookup(ctx, network, host)
	if err != nil || len(ips) == 0 {
		return ips, err
	}
	for _, ip := range ips {
		if !imageFakeIPRange.Contains(ip.Unmap()) {
			return ips, nil
		}
	}
	hostname := imageDNSHostname(host)
	if hostname == "" {
		return ips, nil
	}
	resolved, err := resolve(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("Fake-IP 公网解析失败: %w", err)
	}
	if len(resolved) == 0 {
		return nil, errors.New("Fake-IP 公网解析未返回可用地址")
	}
	return resolved, nil
}

func queryImageDNS(ctx context.Context, client *http.Client, host string, recordType dnsmessage.Type) ([]netip.Addr, error) {
	query := url.Values{"name": {host}, "type": {fmt.Sprint(uint16(recordType))}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare-dns.com/dns-query?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("图片域名解析失败: HTTP %d", resp.StatusCode)
	}
	var answer struct {
		Status   *int `json:"Status"`
		TC       bool `json:"TC"`
		Question []struct {
			Name string          `json:"name"`
			Type dnsmessage.Type `json:"type"`
		} `json:"Question"`
		Answer []struct {
			Type dnsmessage.Type `json:"type"`
			Data string          `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil {
		return nil, err
	}
	if answer.Status == nil || *answer.Status != 0 || answer.TC || len(answer.Question) != 1 || answer.Question[0].Type != recordType ||
		!strings.EqualFold(strings.TrimSuffix(answer.Question[0].Name, "."), host) {
		return nil, errors.New("图片域名解析响应无效")
	}
	var ips []netip.Addr
	for _, item := range answer.Answer {
		ip, err := netip.ParseAddr(item.Data)
		if item.Type == recordType && err == nil && ip.Zone() == "" &&
			((recordType == dnsmessage.TypeA && ip.Is4()) || (recordType == dnsmessage.TypeAAAA && ip.Is6() && !ip.Is4In6())) &&
			isPublicDownloadIP(ip) {
			ips = append(ips, ip)
		}
	}
	return ips, nil
}
