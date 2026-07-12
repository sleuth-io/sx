package main

import (
	"errors"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Benchmark storage for extensions (SxAPI 1.10.0, docs/benchmarks-spec.md):
// one capped, newest-first record list per asset, unified across vault
// types — .sx/benchmarks/<asset>.json on file vaults, real EvalBenchmark
// rows behind the interchange API on skills.new.

var errBenchmarksUnsupported = errors.New(
	"this library's backend doesn't support benchmark storage yet")

func (a *App) benchmarkStore() (vaultpkg.BenchmarkStore, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	store, ok := v.(vaultpkg.BenchmarkStore)
	if !ok {
		return nil, errBenchmarksUnsupported
	}
	return store, nil
}

// PluginBenchmarksList returns an asset's benchmark records as a JSON
// array, newest first ("" when none exist).
func (a *App) PluginBenchmarksList(asset string) (string, error) {
	store, err := a.benchmarkStore()
	if err != nil {
		return "", err
	}
	return store.ListBenchmarks(a.ctx, asset)
}

// PluginBenchmarksAdd records one benchmark result for an asset.
func (a *App) PluginBenchmarksAdd(asset, record string) error {
	store, err := a.benchmarkStore()
	if err != nil {
		return err
	}
	return store.AddBenchmark(a.ctx, asset, record)
}

// PluginBenchmarksLatest returns the newest record per asset as a JSON
// object ("" when nothing is benchmarked yet).
func (a *App) PluginBenchmarksLatest() (string, error) {
	store, err := a.benchmarkStore()
	if err != nil {
		return "", err
	}
	return store.LatestBenchmarks(a.ctx)
}
