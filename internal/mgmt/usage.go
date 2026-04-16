package mgmt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// UsageDirName is the directory under the vault root that holds monthly
// usage JSONL files.
const UsageDirName = ".sx/usage"

// UsageEvent is a single row in .sx/usage/YYYY-MM.jsonl. Timestamp and
// actor are normalized to UTC and lowercase respectively at append time.
type UsageEvent struct {
	Timestamp    time.Time `json:"ts"`
	Actor        string    `json:"actor"`
	AssetName    string    `json:"asset_name"`
	AssetVersion string    `json:"asset_version"`
	AssetType    string    `json:"asset_type"`
}

// UsageFilter narrows a usage query.
type UsageFilter struct {
	AssetName string
	AssetType string
	Actor     string
	Since     time.Time
	Until     time.Time
}

// UsageSummary is the aggregated result of a usage query.
type UsageSummary struct {
	TotalEvents int
	PerAsset    []AssetUsageCount
	PerActor    []ActorUsageCount
}

// AssetUsageCount is the per-asset rollup in a UsageSummary.
type AssetUsageCount struct {
	AssetName    string
	AssetType    string
	TotalUses    int
	UniqueActors int
	LastUsed     time.Time
}

// ActorUsageCount is the per-actor rollup in a UsageSummary.
type ActorUsageCount struct {
	Actor     string
	TotalUses int
}

// AppendUsageEvents appends a batch of usage events to the monthly files
// for their timestamps. Events with a zero timestamp are stamped with the
// current UTC time. Events are grouped by month and written with one
// O_APPEND handle per month to minimize syscalls.
func AppendUsageEvents(vaultRoot string, events []UsageEvent) error {
	if len(events) == 0 {
		return nil
	}

	dir := filepath.Join(vaultRoot, UsageDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create usage directory: %w", err)
	}

	byMonth := make(map[string][]UsageEvent)
	for _, ev := range events {
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now().UTC()
		} else {
			ev.Timestamp = ev.Timestamp.UTC()
		}
		ev.Actor = NormalizeEmail(ev.Actor)
		byMonth[monthFile(ev.Timestamp)] = append(byMonth[monthFile(ev.Timestamp)], ev)
	}

	for name, monthEvents := range byMonth {
		if err := appendJSONL(filepath.Join(dir, name), monthEvents); err != nil {
			return err
		}
	}
	return nil
}

// ReadUsageEvents reads every event from the vault's usage directory,
// filtered by the given filter. Returned events are not sorted.
func ReadUsageEvents(vaultRoot string, filter UsageFilter) ([]UsageEvent, error) {
	events, err := readMonthlyJSONLDir[UsageEvent](filepath.Join(vaultRoot, UsageDirName))
	if err != nil {
		return nil, err
	}
	var out []UsageEvent
	for _, ev := range events {
		if matchesUsageFilter(ev, filter) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// SummarizeUsage computes per-asset and per-actor rollups over the matching
// events. The slices are returned sorted by TotalUses descending.
func SummarizeUsage(vaultRoot string, filter UsageFilter) (*UsageSummary, error) {
	events, err := ReadUsageEvents(vaultRoot, filter)
	if err != nil {
		return nil, err
	}

	type assetKey struct{ name, typ string }
	assetAgg := make(map[assetKey]*AssetUsageCount)
	assetActors := make(map[assetKey]map[string]struct{})
	actorAgg := make(map[string]int)

	for _, ev := range events {
		k := assetKey{ev.AssetName, ev.AssetType}
		if assetAgg[k] == nil {
			assetAgg[k] = &AssetUsageCount{AssetName: ev.AssetName, AssetType: ev.AssetType}
			assetActors[k] = make(map[string]struct{})
		}
		agg := assetAgg[k]
		agg.TotalUses++
		if ev.Timestamp.After(agg.LastUsed) {
			agg.LastUsed = ev.Timestamp
		}
		if ev.Actor != "" {
			assetActors[k][ev.Actor] = struct{}{}
			actorAgg[ev.Actor]++
		}
	}

	summary := &UsageSummary{TotalEvents: len(events)}
	for k, agg := range assetAgg {
		agg.UniqueActors = len(assetActors[k])
		summary.PerAsset = append(summary.PerAsset, *agg)
	}
	sort.Slice(summary.PerAsset, func(i, j int) bool {
		if summary.PerAsset[i].TotalUses != summary.PerAsset[j].TotalUses {
			return summary.PerAsset[i].TotalUses > summary.PerAsset[j].TotalUses
		}
		return summary.PerAsset[i].AssetName < summary.PerAsset[j].AssetName
	})

	for actor, count := range actorAgg {
		summary.PerActor = append(summary.PerActor, ActorUsageCount{Actor: actor, TotalUses: count})
	}
	sort.Slice(summary.PerActor, func(i, j int) bool {
		if summary.PerActor[i].TotalUses != summary.PerActor[j].TotalUses {
			return summary.PerActor[i].TotalUses > summary.PerActor[j].TotalUses
		}
		return summary.PerActor[i].Actor < summary.PerActor[j].Actor
	})

	return summary, nil
}

func matchesUsageFilter(ev UsageEvent, f UsageFilter) bool {
	if f.AssetName != "" && ev.AssetName != f.AssetName {
		return false
	}
	if f.AssetType != "" && ev.AssetType != f.AssetType {
		return false
	}
	if f.Actor != "" && !strings.EqualFold(ev.Actor, f.Actor) {
		return false
	}
	if !f.Since.IsZero() && ev.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Timestamp.After(f.Until) {
		return false
	}
	return true
}
