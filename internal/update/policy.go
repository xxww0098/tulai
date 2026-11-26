package update

import (
	"strings"
	"time"
)

const (
	// CheckEvery is how often a running app looks for a release after startup.
	CheckEvery = 6 * time.Hour

	// KeyAutoCheck is the settings-table key for 「自动检查更新」. "1" is on.
	KeyAutoCheck = "auto_check_updates"
	// KeyMirror is an optional ghproxy-style prefix. Empty uses DefaultMirrors.
	KeyMirror = "update_mirror"
	// KeySkipped is the release the user chose with 「跳过此版本」.
	KeySkipped = "update_skipped_version"

	// DeferMessage is shown when a generation is still running.
	DeferMessage = "任务进行中，结束后将自动更新"
)

// DefaultMirrors are tried in multi-source racing and failover for GitHub Releases.
// A settings value replaces the top preference; "off" uses GitHub only.
var DefaultMirrors = []string{
	"https://edgeone.gh-proxy.org/",
	"https://cdn.gh-proxy.org/",
	"https://gh-proxy.org/",
	"https://ghfast.top/",
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
}

// KV is the settings store. *store.Store implements it.
type KV interface {
	Get(key string) string
	Set(key, value string)
}

// AutoOn reports whether background checks should run. Empty means on.
func AutoOn(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "true", "yes", "on", "开":
		return true
	default:
		return false
	}
}

// HasExplicitMirror reports whether setting configures one or more explicit mirror prefixes.
func HasExplicitMirror(setting string) bool {
	setting = strings.TrimSpace(setting)
	switch strings.ToLower(setting) {
	case "", "__custom__", "off", "direct", "-", "github", "none", "关闭":
		return false
	default:
		for _, part := range strings.Split(setting, ",") {
			if strings.TrimSpace(part) != "" {
				return true
			}
		}
		return false
	}
}

// MirrorSetting turns the settings value into mirror prefixes.
// Empty keeps the defaults. "off" (and a few aliases) disables mirrors.
// Custom prefixes (single or comma-separated) are placed first, followed by defaults.
func MirrorSetting(setting string, defaults []string) []string {
	setting = strings.TrimSpace(setting)
	switch strings.ToLower(setting) {
	case "off", "direct", "-", "github", "none", "关闭":
		return nil
	case "", "__custom__":
		return append([]string{}, defaults...)
	default:
		out := make([]string, 0, len(defaults)+2)
		seen := make(map[string]bool, len(defaults)+2)
		for _, part := range strings.Split(setting, ",") {
			item := strings.TrimSpace(part)
			if item == "" {
				continue
			}
			key := strings.TrimRight(item, "/")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item)
		}
		for _, item := range defaults {
			trimmed := strings.TrimSpace(item)
			key := strings.TrimRight(trimmed, "/")
			if trimmed == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item)
		}
		return out
	}
}

// ShouldOffer reports whether latest should be shown. A skipped release stays
// hidden until a different newer version is published.
func ShouldOffer(current, latest, skipped string) (bool, error) {
	newer, err := Newer(current, latest)
	if err != nil || !newer {
		return false, err
	}
	skipped = strings.TrimSpace(skipped)
	if skipped == "" {
		return true, nil
	}
	if _, err := ParseVersion(skipped); err != nil {
		return true, nil
	}
	cmp, err := Compare(latest, skipped)
	if err != nil {
		return true, nil
	}
	return cmp != 0, nil
}

// RememberSkip stores the canonical version the user does not want installed.
func RememberSkip(kv KV, version string) error {
	if kv == nil {
		return errNoStore
	}
	parsed, err := ParseVersion(version)
	if err != nil {
		return err
	}
	kv.Set(KeySkipped, parsed)
	return nil
}

// Skipped reads the stored version. A missing store or key is empty.
func Skipped(kv KV) string {
	if kv == nil {
		return ""
	}
	return strings.TrimSpace(kv.Get(KeySkipped))
}

// Step is what to do with a pending update.
type Step string

const (
	StepIdle     Step = "idle"
	StepDefer    Step = "defer"
	StepDownload Step = "download"
	StepInstall  Step = "install"
)

// NextStep keeps a running generation uninterrupted.
// pending is the user asking to update (or a deferred request).
// downloaded means the verified asset is already on disk.
func NextStep(running, pending, downloaded bool) Step {
	if !pending {
		return StepIdle
	}
	if running {
		return StepDefer
	}
	if downloaded {
		return StepInstall
	}
	return StepDownload
}

// ShortNotes keeps the release text short enough for the banner.
func ShortNotes(body string, max int) string {
	body = strings.TrimSpace(body)
	if body == "" || max <= 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	text := b.String()
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// CheckDue reports whether interval has passed since last. A zero last is due.
func CheckDue(last, now time.Time, every time.Duration) bool {
	if last.IsZero() || every <= 0 {
		return true
	}
	return !now.Before(last.Add(every))
}
