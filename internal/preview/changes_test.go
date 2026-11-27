package preview

import (
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

func TestSecretElementSelectorNotShown(t *testing.T) {
	const value = "PSFIXTURE-SELECTOR-7K1D"
	key := "/hooks/Stop@/hooks/0/command=notify --token " + value
	c := plan.FileChange{
		Path:    "/x/settings.json",
		Units:   []plan.Unit{{Kind: manifest.UnitJSONElement, Key: key, Content: []byte(`{"hooks":[]}`), Secret: true}},
		Removed: []manifest.Entry{{Unit: manifest.UnitJSONElement, Key: key, Secret: true}},
	}

	out := Change(c)

	if strings.Contains(out, value) {
		t.Errorf("preview holds the secret selector:\n%s", out)
	}
	if !strings.Contains(out, "@@ set /hooks/Stop@(secret) @@") || !strings.Contains(out, "@@ remove /hooks/Stop@(secret) @@") {
		t.Errorf("preview does not name the array:\n%s", out)
	}
	if got := UnitName(manifest.UnitJSONElement, key, false); got != key {
		t.Errorf("plain unit name %q", got)
	}
}
