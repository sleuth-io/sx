# Quality storage

One quality store per library, unified across vault types (SxAPI
1.12.0, the `quality` permission). Quality is a per-asset evaluation of
the skill itself — an overall 0–100 score, four category scores, a
summary, and insights — matching the Quality tab on skills.new. The
skill-quality extension surfaces it; on skills.new the store is the
server's own evaluation, read as-is.

## Where records live — and who evaluates

| Vault type | Storage | Who evaluates |
|---|---|---|
| Local folder / synced folder / git | `.sx/quality/<asset>.json` — `{"quality": [...]}`, newest first, capped at 10 records per asset (git history keeps the rest) | The extension, through the user's AI provider (`reevaluate` returns `"local"`); it stores the record via `add` |
| skills.new | The server's own evaluation document (`Asset.evaluation_result`), read through `vault { assets }` and normalized to the interchange shape; one record, no history | The server (`reevaluate` fires the `evaluateAsset` mutation and returns `"server"`; poll `get` until `evaluating` flips false). `add` is refused — the server document is the source of truth |

File vaults enforce the same contract before anything is written: one
JSON object per record, at most 256 KB, with a numeric `overall` in
[0,100] and a `categories` object. Servers predating the surface yield
"doesn't support quality storage yet" — extensions should
feature-detect and fall back. skills.new only evaluates managed assets;
git-sourced assets there have no quality data.

## The interchange record

Normalized from pulse's evaluation document
(`asset_evaluation_tool.py`): `overall_confidence`×100 → `overall`,
`category_scores.content_quality` → `categories.content`, `reasoning` →
`summary`, `llm_strengths`/`llm_weaknesses`/`llm_recommendations` →
`insights`.

```json
{
  "at": "2026-07-14T18:00:00Z",
  "source": "app",
  "by": "detkin@sleuth.io",
  "executor": { "provider": "claude-cli", "model": "claude-sonnet-4-6" },
  "overall": 84,
  "categories": { "structure": 90, "actionability": 80, "content": 83, "completeness": 85 },
  "factors": {
    "specificity": { "score": 90, "tier": "Excellent", "justification": "..." }
  },
  "summary": "A solid, well-structured skill that...",
  "insights": {
    "strengths": ["Excellent concrete examples..."],
    "improvements": ["No troubleshooting section..."],
    "recommendations": ["Add a Setup section..."]
  },
  "stats": { "file_count": 2, "word_count": 609 },
  "skill_hash": "a1b2c3d4"
}
```

- `source` — `"app"` for extension-run records, `"server"` for
  evaluations skills.new ran itself (the only kind that vault returns).
- Staleness: app records carry `skill_hash` (the sx content hash they
  evaluated); server records carry `at` approximated from the asset's
  update time, since pulse stores no evaluation timestamp.
- The trend chip (score delta vs the previous record) only exists on
  file vaults — skills.new keeps a single document, so its history has
  at most one record.
- Calibration note: the extension's local rubric is a port of pulse's
  and both sides evolve independently — records are comparable in
  shape, not calibration. Don't chart app and server scores as one
  series.

## Extension API

```ts
sx.quality.get(assetName)   // { evaluating, records: QualityRecord[] } — newest first
sx.quality.add(assetName, record)   // file vaults only; skills.new refuses
sx.quality.latest()         // { [assetName]: QualityRecord } — one bulk read
sx.quality.reevaluate(assetName)    // { mode: "server" | "local" }
```

The `reevaluate` contract keeps vault-type knowledge out of extensions:
`"server"` means the backend is evaluating (poll `get`); `"local"`
means the extension evaluates via `sx.llm` and stores via `add`.

Any library member may read and record on file vaults — the same trust
model as `storage:shared`. On skills.new, re-evaluation runs under the
member's own account and the server enforces edit rights.
