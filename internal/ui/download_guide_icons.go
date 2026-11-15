package ui

import (
	"embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Bundled favicons keep the guide usable without a network connection.
//
//go:embed assets/download-sites/*.png
var downloadGuideIconFiles embed.FS

var downloadGuideIcons = func() map[string]*ui.Bitmap {
	icons := make(map[string]*ui.Bitmap)
	files, _ := downloadGuideIconFiles.ReadDir("assets/download-sites")
	for _, file := range files {
		data, err := downloadGuideIconFiles.ReadFile("assets/download-sites/" + file.Name())
		if err != nil {
			continue
		}
		if icon, err := ui.DecodeBitmap(data); err == nil {
			icons[strings.TrimSuffix(file.Name(), ".png")] = icon
		}
	}
	return icons
}()

func downloadGuideSiteIcons(c *ui.Context, hosts string, tok tokens) {
	domains := strings.Fields(hosts)
	if len(domains) == 0 {
		domains = []string{""}
	}
	size := float32(18)
	if len(domains) == 1 {
		size = 28
	}
	ui.Column(c).Width(58).Shrink(0).Gap(2).AlignItems(ui.Center).Children(func() {
		for start := 0; start < len(domains); start += 3 {
			ui.Row(c).Gap(2).Children(func() {
				for _, domain := range domains[start:min(start+3, len(domains))] {
					icon := downloadGuideIcons[domain]
					tileBg := tok.sunken
					if icon != nil {
						tileBg = tok.card
						if tok.dark {
							// 透明底深色站点图标在暗色卡片上需要柔和浅色托盘，避免与深色背景融为一体。
							tileBg = tok.text.Alpha(0.92)
						}
					}
					tile := ui.Column(c).Size(size, size).Radius(tok.radiusS+1).Center().Shrink(0).
						Background(tileBg).Border(1, tok.border)
					if domain != "" {
						tile.Tooltip(domain)
					}
					tile.Children(func() {
						if icon != nil {
							ui.Image(c, icon).Size(size-4, size-4).Fit(ui.Contain)
						} else {
							ui.Icon(c, iconGlobe).FontSize(size - 8).TextColor(tok.muted)
						}
					})
				}
			})
		}
	})
}
