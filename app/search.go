package main

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Full-text content search behind the main search box. Name/description
// matching stays instant in the frontend; this covers what that can't —
// hits inside asset markdown — with an excerpt around the best hit.
// Bundles are cached in memory per name@version, so the first search
// pays the vault reads and the rest are local.

// ContentMatch is one asset whose markdown matched the query.
type ContentMatch struct {
	Name    string `json:"name"`
	Matches int    `json:"matches"`
	// Excerpt around the first hit; Match is the matched text itself so
	// the UI can highlight it.
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
	score  float64
}

const (
	contentSearchLimit = 50
	searchConcurrency  = 8
	excerptRadius      = 60
)

// SearchAssetContent scans every asset's markdown for the query and
// returns ranked matches. Heading hits weigh more than body hits.
// Unreachable or malformed assets are skipped — search must degrade,
// not fail, on one bad asset.
func (a *App) SearchAssetContent(query string) ([]ContentMatch, error) {
	out := []ContentMatch{}
	terms, phrases := parseContentQuery(query)
	if len(terms) == 0 && len(phrases) == 0 {
		return out, nil
	}
	v, err := a.currentVault()
	if err != nil {
		return out, err
	}
	res, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Limit: 500})
	if err != nil {
		return out, friendlyVaultError(err)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	work := make(chan vaultpkg.AssetSummary)
	for range searchConcurrency {
		wg.Go(func() {
			for summary := range work {
				text := a.assetMarkdown(v, summary)
				if text == "" {
					continue
				}
				if m, ok := scoreContent(summary.Name, text, terms, phrases); ok {
					mu.Lock()
					out = append(out, m)
					mu.Unlock()
				}
			}
		})
	}
	for _, summary := range res.Assets {
		// Extensions are invisible outside the Extensions screen.
		if summary.Type.Key == asset.TypeAppPlugin.Key {
			continue
		}
		work <- summary
	}
	close(work)
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > contentSearchLimit {
		out = out[:contentSearchLimit]
	}
	return out, nil
}

// assetMarkdown returns the concatenated markdown of an asset's latest
// revision, from the in-memory cache when the version hasn't changed.
// Superseded revisions are evicted on the spot — without that, every
// republish would leave the old blob resident for the app's lifetime.
func (a *App) assetMarkdown(v vaultpkg.Vault, summary vaultpkg.AssetSummary) string {
	key := summary.Name + "@" + summary.LatestVersion
	if cached, ok := a.searchCache.Load(key); ok {
		return cached.(string)
	}
	if oldKey, ok := a.searchCacheKeys.Load(summary.Name); ok && oldKey != key {
		a.searchCache.Delete(oldKey.(string))
	}
	zipData, err := latestZipFromVault(a.ctx, v, summary.Name)
	if err != nil {
		return ""
	}
	entries, err := utils.ListZipEntries(zipData)
	if err != nil {
		return ""
	}
	var parts []string
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			continue
		}
		if content, err := utils.ReadZipFile(zipData, entry.Name); err == nil {
			parts = append(parts, string(content))
		}
	}
	text := strings.Join(parts, "\n")
	a.searchCache.Store(key, text)
	a.searchCacheKeys.Store(summary.Name, key)
	return text
}

// purgeSearchCache drops an asset's cached markdown — called when the
// asset is deleted so removed content can't linger in memory (or keep
// matching searches for a beat after deletion).
func (a *App) purgeSearchCache(name string) {
	if key, ok := a.searchCacheKeys.Load(name); ok {
		a.searchCache.Delete(key.(string))
		a.searchCacheKeys.Delete(name)
	}
}

// parseContentQuery splits a query into loose terms and "quoted phrases",
// lowercased. Terms under two characters are dropped as noise.
func parseContentQuery(query string) (terms, phrases []string) {
	rest := query
	for {
		start := strings.IndexByte(rest, '"')
		if start < 0 {
			break
		}
		end := strings.IndexByte(rest[start+1:], '"')
		if end < 0 {
			break
		}
		if p := strings.TrimSpace(strings.ToLower(rest[start+1 : start+1+end])); p != "" {
			phrases = append(phrases, p)
		}
		rest = rest[:start] + " " + rest[start+2+end:]
	}
	for w := range strings.FieldsSeq(strings.ToLower(rest)) {
		if len(w) >= 2 {
			terms = append(terms, w)
		}
	}
	return terms, phrases
}

// scoreContent scores one asset's markdown: every term and phrase must
// appear (AND semantics — multi-word queries narrow, like every search
// box people know), heading lines weigh 4× body. Matching runs on the
// ORIGINAL text via case-insensitive regexp — offsets from a lowercased
// copy can misalign (ToLower may change byte length), which would
// highlight the wrong characters.
func scoreContent(name, text string, terms, phrases []string) (ContentMatch, bool) {
	var headings []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headings = append(headings, line)
		}
	}
	headingText := strings.Join(headings, "\n")

	score := 0.0
	total := 0
	firstHit := -1
	firstLen := 0
	consider := func(needle string, weight float64) bool {
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(needle))
		if err != nil {
			return false
		}
		locs := re.FindAllStringIndex(text, -1)
		if len(locs) == 0 {
			return false
		}
		total += len(locs)
		score += weight * float64(len(locs))
		score += 3 * weight * float64(len(re.FindAllStringIndex(headingText, -1)))
		if firstHit < 0 || locs[0][0] < firstHit {
			firstHit = locs[0][0]
			firstLen = locs[0][1] - locs[0][0]
		}
		return true
	}
	for _, p := range phrases {
		if !consider(p, 4) {
			return ContentMatch{}, false
		}
	}
	for _, t := range terms {
		if !consider(t, 1) {
			return ContentMatch{}, false
		}
	}
	if total == 0 {
		return ContentMatch{}, false
	}

	// The radius is in bytes; snap outward to rune boundaries so the
	// excerpt never opens or closes mid-character (em-dashes and smart
	// quotes are everyday markdown).
	start := max(0, firstHit-excerptRadius)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(len(text), firstHit+firstLen+excerptRadius)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	m := ContentMatch{
		Name:    name,
		Matches: total,
		Before:  strings.ReplaceAll(text[start:firstHit], "\n", " "),
		Match:   text[firstHit : firstHit+firstLen],
		After:   strings.ReplaceAll(text[firstHit+firstLen:end], "\n", " "),
		score:   score,
	}
	if start > 0 {
		m.Before = "…" + m.Before
	}
	if end < len(text) {
		m.After += "…"
	}
	return m, true
}
