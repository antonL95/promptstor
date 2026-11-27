package ci_test

import (
	"testing"

	"github.com/antonL95/promptstor/internal/testenv"
)

func TestCheckCIWithHandWrittenItem(t *testing.T) {
	root := repo(t)
	replace(t, root, ".promptstor/rules/plain-language.md", "promptstor:\n  id: ps_00000000000000000000000C11\n", "")
	replace(t, root, ".promptstor/mcp/example-docs.yaml", "promptstor:\n  id: ps_00000000000000000000000C14\n", "")
	before := testenv.TreeHash(t, root)

	code, out := check(t, root)

	if code != 0 || out != inSync {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if testenv.TreeHash(t, root) != before {
		t.Error("the check wrote into the repository")
	}

	write(t, root, ".promptstor/rules/added-by-hand.md", "Name tests after behaviour.\n")
	before = testenv.TreeHash(t, root)

	code, out = check(t, root)

	if code != 1 {
		t.Errorf("exit %d:\n%s", code, out)
	}
	wantAll(t, out, "+Name tests after behaviour.")
	if testenv.TreeHash(t, root) != before {
		t.Error("the check wrote into the repository")
	}
}
