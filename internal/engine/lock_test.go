package engine_test

import (
	"sync"
	"testing"
	"time"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/engine"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/plan"
)

func TestInterleavedSyncsKeepBothManifestEntries(t *testing.T) {
	env := home(t)
	firstPath, secondPath := at(env, ".example/first.md"), at(env, ".example/second.md")
	planning := make(chan struct{})
	resume := make(chan struct{})
	var entered sync.Once
	first := newEngine(env, adapter.Codex)
	first.Adapters = []adapter.Adapter{&recording{id: adapter.Codex, units: func(adapter.Target) []plan.Unit {
		entered.Do(func() { close(planning) })
		<-resume
		return []plan.Unit{fileUnit(env, adapter.Codex, ".example/first.md", "first\n", ruleID)}
	}}}
	second := newEngine(env, adapter.ClaudeCode)
	second.Adapters = []adapter.Adapter{&recording{id: adapter.ClaudeCode, units: func(adapter.Target) []plan.Unit {
		return []plan.Unit{fileUnit(env, adapter.ClaudeCode, ".example/second.md", "second\n", skillID)}
	}}}

	firstDone := syncAsync(first)
	<-planning
	secondDone := syncAsync(second)
	select {
	case err := <-secondDone:
		t.Logf("second sync finished while the first held its plan: %v", err)
		secondDone = finished(err)
	case <-time.After(300 * time.Millisecond):
	}
	close(resume)

	if err := <-firstDone; err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second sync: %v", err)
	}
	assertOwned(t, env, firstPath, adapter.Codex)
	assertOwned(t, env, secondPath, adapter.ClaudeCode)
}

func syncAsync(e engine.Engine) chan error {
	done := make(chan error, 1)
	go func() {
		_, _, err := e.Sync("", plan.Options{})
		done <- err
	}()
	return done
}

func finished(err error) chan error {
	done := make(chan error, 1)
	done <- err
	return done
}

func assertOwned(t *testing.T, env paths.Env, path, tool string) {
	t.Helper()
	m, err := manifest.Load(env.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.Path == path && e.Tool == tool {
			return
		}
	}
	t.Errorf("manifest lost the %s entry for %s", tool, path)
}
