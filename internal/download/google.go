package download

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

func googleSite(page *url.URL) string {
	if page == nil {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(page.Hostname()))
	if err != nil || !strings.HasPrefix(domain, "google.") {
		return ""
	}
	return domain
}

func googleVerificationPage(page *url.URL) bool {
	return googleSite(page) != "" && (page.Path == "/sorry" || strings.HasPrefix(page.Path, "/sorry/"))
}

func googleBlockedPage(root, page *url.URL, doc *html.Node) bool {
	if googleSite(root) == "" || googleSite(root) != googleSite(page) {
		return false
	}
	if googleVerificationPage(page) {
		return true
	}
	if root.Query().Get("tbm") != "isch" && root.Query().Get("udm") != "2" {
		return false
	}
	needsJS, hasImages := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "img" && attribute(n, "src") != "" {
			hasImages = true
		}
		if n.Data == "a" {
			if u, err := crawlURL(page, attribute(n, "href")); err == nil && sameOrigin(page, u) && u.Path == "/httpservice/retry/enablejs" {
				needsJS = true
			}
		}
	})
	return needsJS && !hasImages
}
