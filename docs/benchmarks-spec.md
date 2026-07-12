# Benchmark storage

One benchmark store per library, unified across vault types (SxAPI
1.10.0, the `benchmarks` permission). The skill-evals extension runs
with-skill vs without-skill benchmarks through the user's AI provider
and records the results here; on skills.new the same store also surfaces
benchmarks the server ran itself.

## Where records live

| Vault type | Storage |
|---|---|
| Local folder / synced folder / git | `.sx/benchmarks/<asset>.json` — `{"benchmarks": [...]}`, newest first, capped at 20 records per asset (git history keeps the rest) |
| skills.new | Real `EvalBenchmark` rows (`triggered_by=external`), read back through `vault { assetBenchmarks }` / `vault { latestAssetBenchmarks }`, written through `importAssetBenchmark` |

Every backend enforces the same contract before anything is written:
one JSON object per record, at most 256 KB, carrying a `summary`.
Servers predating the surface yield "doesn't support benchmark storage
yet" — extensions should feature-detect and fall back.

## The interchange record

The same document a skills.new server-run benchmark exports; the
aggregate is pulse's `run_summary` shape verbatim.

```json
{
  "at": "2026-07-12T18:00:00Z",
  "source": "app",
  "executor": { "provider": "claude-cli", "model": "claude-sonnet-4-6" },
  "runs_per_config": 1,
  "by": "detkin@sleuth.io",
  "summary": {
    "with_skill":    { "pass_rate": { "mean": 0.85, "stddev": 0.12, "min": 0.67, "max": 1.0 } },
    "without_skill": { "pass_rate": { "mean": 0.60, "stddev": 0.18, "min": 0.33, "max": 0.89 } },
    "delta": { "pass_rate": 0.25 }
  },
  "per_eval": [ { "eval_key": "basic-commit", "with_pass": 1.0, "without_pass": 0.5, "status": "passing" } ],
  "notes": ["strong skill impact"],
  "skill_hash": "a1b2c3d4",
  "skill_version": "4",
  "is_current_version": true
}
```

- `source` — `"app"` for client-run records, `"server"` for benchmarks
  skills.new executed itself.
- Staleness: app records carry `skill_hash` (the sx content hash they
  ran against); server records carry `skill_version` plus the
  server-computed `is_current_version`.
- `per_eval` is optional; server records may omit it.

## Extension API

```ts
sx.benchmarks.list(assetName)  // BenchmarkRecord[], newest first
sx.benchmarks.add(assetName, record)
sx.benchmarks.latest()         // { [assetName]: BenchmarkRecord } — one bulk read
```

Any library member may read and record — the same trust model as
`storage:shared`, where any member's app writes the file. Server-run
benchmarks on skills.new remain admin-triggered.
