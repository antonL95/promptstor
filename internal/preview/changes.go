package preview

import (
	"strings"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const secretNotShown = "(secret, not shown)\n"

func Change(c plan.FileChange) string {
	if !mayHoldSecrets(c) {
		return UnifiedDiff(c.Path, c.Before, c.After)
	}
	var out strings.Builder
	out.WriteString("--- " + c.Path + "\n+++ " + c.Path + "\n")
	for _, u := range c.Units {
		out.WriteString("@@ set " + unitName(u.Kind, u.Key) + " @@\n")
		if u.Secret {
			out.WriteString(secretNotShown)
			continue
		}
		for _, line := range splitLines(string(u.Content)) {
			out.WriteString("+" + strings.TrimSuffix(line, "\n") + "\n")
		}
	}
	for _, e := range c.Removed {
		out.WriteString("@@ remove " + unitName(e.Unit, e.Key) + " @@\n")
	}
	return out.String()
}

func mayHoldSecrets(c plan.FileChange) bool {
	for _, u := range c.Units {
		if u.Secret || structured(u.Kind) {
			return true
		}
	}
	for _, e := range c.Removed {
		if e.Secret || structured(e.Unit) {
			return true
		}
	}
	return false
}

func structured(kind manifest.UnitKind) bool {
	return kind != manifest.UnitFile && kind != manifest.UnitSection
}

func unitName(kind manifest.UnitKind, key string) string {
	if key == "" {
		return string(kind)
	}
	return key
}
