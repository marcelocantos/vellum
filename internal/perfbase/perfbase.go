// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package perfbase ratchets allocation metrics for the pure-Go
// conversion paths against docs/perf/baseline.yaml.
//
// Wall time is too noisy to gate on, but allocation count and bytes
// for a fixed input are deterministic enough to lock within a band.
// The lock holds in both directions: a regression fails, and an
// improvement also fails until the baseline is deliberately re-recorded,
// so the number only ever moves by a commit that says so.
//
// The race detector changes allocation behaviour, so the baseline holds
// one section per build mode (race, norace) and each run checks the
// section for the mode it was built with. Re-record both with:
//
//	VELLUM_PERF_RECORD=1 go test -p 1 -race -run Ratchet ./convert ./importer
//	VELLUM_PERF_RECORD=1 go test -p 1 -run Ratchet ./convert ./importer
//
// (-p 1 because every package rewrites the same file.)
package perfbase

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// RecordEnv, when set to 1, rewrites the baseline with the measured
// values instead of checking against them.
const RecordEnv = "VELLUM_PERF_RECORD"

// Runs is how many timed executions each metric averages over, after
// one untimed warm-up. Two is enough: allocation counts for a fixed
// input barely move between runs, and the gate pays for every run
// under the race detector.
const Runs = 2

// File is the baseline path relative to the repository root.
const File = "docs/perf/baseline.yaml"

// Metric is one locked measurement.
type Metric struct {
	Allocs uint64 `yaml:"allocs"`
	Bytes  uint64 `yaml:"bytes"`
}

// Baseline is the on-disk shape of docs/perf/baseline.yaml.
type Baseline struct {
	// TolerancePercent is the permitted deviation either side of the
	// locked value.
	TolerancePercent float64 `yaml:"tolerance_percent"`
	// Metrics is keyed by build mode ("race" | "norace"), then metric.
	Metrics map[string]map[string]Metric `yaml:"metrics"`
}

// Mode names the build mode this binary was compiled in; see race.go
// and norace.go.
func Mode() string {
	if raceEnabled {
		return "race"
	}
	return "norace"
}

const header = `# Allocation baseline for the pure-Go conversion paths, locked in both
# directions by the perf ratchet tests (internal/perfbase). Numbers are
# per operation on the internal/perffixture inputs, per build mode.
#
# Do not hand-edit. Re-record deliberately, in the commit that changes
# the measured code, once per build mode:
#
#   VELLUM_PERF_RECORD=1 go test -p 1 -race -run Ratchet ./convert ./importer
#   VELLUM_PERF_RECORD=1 go test -p 1 -run Ratchet ./convert ./importer
#
# Wall-clock numbers live in docs/perf/baseline.md.
`

// Measure runs fn once untimed, then Runs times, and returns the mean
// allocation count and bytes per run.
func Measure(fn func()) Metric {
	fn()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < Runs; i++ {
		fn()
	}
	runtime.ReadMemStats(&after)
	return Metric{
		Allocs: (after.Mallocs - before.Mallocs) / Runs,
		Bytes:  (after.TotalAlloc - before.TotalAlloc) / Runs,
	}
}

// Ratchet measures fn and checks it against the named baseline metric,
// or records it when RecordEnv is set. A missing metric fails unless
// recording, so a new benchmark cannot silently run unlocked.
func Ratchet(t *testing.T, name string, fn func()) {
	t.Helper()
	got := Measure(fn)
	path := locate(t)
	if os.Getenv(RecordEnv) == "1" {
		record(t, path, name, got)
		t.Logf("recorded %s/%s: allocs=%d bytes=%d", Mode(), name, got.Allocs, got.Bytes)
		return
	}
	base := load(t, path)
	want, ok := base.Metrics[Mode()][name]
	if !ok {
		t.Fatalf("no %s baseline for %q in %s; record one with %s=1", Mode(), name, File, RecordEnv)
	}
	check(t, name, "allocs", got.Allocs, want.Allocs, base.TolerancePercent)
	check(t, name, "bytes", got.Bytes, want.Bytes, base.TolerancePercent)
}

func check(t *testing.T, name, field string, got, want uint64, tolerance float64) {
	t.Helper()
	lo := float64(want) * (1 - tolerance/100)
	hi := float64(want) * (1 + tolerance/100)
	g := float64(got)
	switch {
	case g > hi:
		t.Errorf("%s %s regressed: %d, baseline %d (+%.1f%%, band ±%.0f%%)",
			name, field, got, want, 100*(g/float64(want)-1), tolerance)
	case g < lo:
		t.Errorf("%s %s improved: %d, baseline %d (%.1f%%, band ±%.0f%%); lock it in by re-recording with %s=1",
			name, field, got, want, 100*(g/float64(want)-1), tolerance, RecordEnv)
	}
}

// locate finds the baseline file from the package directory go test
// runs in, walking up to the module root.
func locate(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, filepath.FromSlash(File))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("module root not found above %s", dir)
		}
		dir = parent
	}
}

func load(t *testing.T, path string) Baseline {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (record one with %s=1)", File, err, RecordEnv)
	}
	var b Baseline
	if err := yaml.Unmarshal(data, &b); err != nil {
		t.Fatalf("parsing %s: %v", File, err)
	}
	if b.TolerancePercent <= 0 {
		t.Fatalf("%s: tolerance_percent must be positive", File)
	}
	return b
}

// DefaultTolerancePercent is written when recording into a fresh file.
const DefaultTolerancePercent = 15

func record(t *testing.T, path, name string, m Metric) {
	t.Helper()
	b := Baseline{TolerancePercent: DefaultTolerancePercent}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &b); err != nil {
			t.Fatalf("parsing %s: %v", File, err)
		}
	}
	if b.Metrics == nil {
		b.Metrics = map[string]map[string]Metric{}
	}
	if b.Metrics[Mode()] == nil {
		b.Metrics[Mode()] = map[string]Metric{}
	}
	b.Metrics[Mode()][name] = m
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(header+"\n"+render(b)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// render writes the baseline with modes and metrics in name order so
// re-recording produces a stable diff.
func render(b Baseline) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "tolerance_percent: %g\nmetrics:\n", b.TolerancePercent)
	for _, mode := range sortedKeys(b.Metrics) {
		fmt.Fprintf(&sb, "  %s:\n", mode)
		for _, n := range sortedKeys(b.Metrics[mode]) {
			m := b.Metrics[mode][n]
			fmt.Fprintf(&sb, "    %s:\n      allocs: %d\n      bytes: %d\n", n, m.Allocs, m.Bytes)
		}
	}
	return sb.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
