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
)

var temuFakeIPRange = netip.MustParsePrefix("198.18.0.0/15")

var temuDNSClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", "1.1.1.1:443")
		},
		TLSHandshakeTimeout: 5 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func lookupTemuImageIPs(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return lookupTemuImageIPsWith(ctx, network, host, net.DefaultResolver.LookupNetIP, resolveTemuImageIPs)
}

func lookupTemuImageIPsWith(ctx context.Context, network, host string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	resolve func(context.Context, string) ([]netip.Addr, error),
) ([]netip.Addr, error) {
	ips, err := lookup(ctx, network, host)
	if err != nil || len(ips) == 0 || !temuImageHost(strings.ToLower(strings.TrimSuffix(host, "."))) {
		return ips, err
	}
	for _, ip := range ips {
		if !temuFakeIPRange.Contains(ip.Unmap()) {
			return ips, nil
		}
	}
	// TUN fake IPs cannot be distinguished from local targets; pin a verified public DNS answer instead.
	if public, err := resolve(ctx, strings.ToLower(strings.TrimSuffix(host, "."))); err == nil && len(public) != 0 {
		return public, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ips, nil
}

func resolveTemuImageIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	return queryTemuImageDNS(ctx, temuDNSClient, host)
}

func queryTemuImageDNS(ctx context.Context, client *http.Client, host string) ([]netip.Addr, error) {
	if !temuImageHost(host) {
		return nil, errors.New("TEMU 图片域名无效")
	}
	query := url.Values{"name": {host}, "type": {"A"}}
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
		return nil, fmt.Errorf("TEMU 图片域名解析失败: HTTP %d", resp.StatusCode)
	}
	var answer struct {
		Status   int `json:"Status"`
		Question []struct {
			Name string `json:"name"`
			Type int    `json:"type"`
		} `json:"Question"`
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil {
		return nil, err
	}
	if answer.Status != 0 || len(answer.Question) != 1 || answer.Question[0].Type != 1 ||
		!strings.EqualFold(strings.TrimSuffix(answer.Question[0].Name, "."), host) {
		return nil, errors.New("TEMU 图片域名解析响应无效")
	}
	var ips []netip.Addr
	for _, item := range answer.Answer {
		ip, err := netip.ParseAddr(item.Data)
		if item.Type == 1 && err == nil && ip.Is4() && isPublicDownloadIP(ip) {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("TEMU 图片域名没有公网地址")
	}
	return ips, nil
}
