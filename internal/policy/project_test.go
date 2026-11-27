package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/antonL95/promptstor/internal/policy"
)

func TestLoadProjectAcksRefusesOnlyALinkedAcksFile(t *testing.T) {
	linkedLayer := func(t *testing.T, content string) string {
		t.Helper()
		root, layer := t.TempDir(), t.TempDir()
		if content != "" {
			if err := os.WriteFile(filepath.Join(layer, "acks.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(layer, filepath.Join(root, ".promptstor")); err != nil {
			t.Fatal(err)
		}
		return root
	}

	if _, err := policy.LoadProjectAcks(linkedLayer(t, "")); err != nil {
		t.Errorf("linked layer without acks: %v", err)
	}
	if _, err := policy.LoadProjectAcks(linkedLayer(t, `{"acks": []}`)); err == nil {
		t.Error("acks were read through a link")
	}
}
