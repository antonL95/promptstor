package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonL95/promptstor/internal/doctor"
	"github.com/antonL95/promptstor/internal/drift"
	"github.com/antonL95/promptstor/internal/paths"
	"github.com/antonL95/promptstor/internal/policy"
	"github.com/antonL95/promptstor/internal/testenv"
)

const handWrittenRule = "Name tests after behaviour.\n"

func handRule(env paths.Env) string {
	return filepath.Join(env.LibraryDir(), "rules", "added-by-hand.md")
}

func TestDoctorDryRunAndCheckNeverWriteIDs(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	write(t, handRule(env), handWrittenRule)
	before := testenv.TreeHash(t, env.LibraryDir())

	run(t, env, &script{answers: []answer{{syncNow, "yes"}}}, doctor.Options{DryRun: true})
	run(t, env, &script{}, doctor.Options{Check: true})

	if testenv.TreeHash(t, env.LibraryDir()) != before {
		t.Error("doctor wrote into the library without applying")
	}
}

func TestDoctorFirstDryRunNeverWritesIDs(t *testing.T) {
	env := fixtureHome(t)
	write(t, handRule(env), handWrittenRule)

	run(t, env, &script{}, doctor.Options{DryRun: true})

	if got := read(t, handRule(env)); got != handWrittenRule {
		t.Errorf("dry run rewrote the item:\n%s", got)
	}
}

func TestDoctorFirstRunWritesIDsIntoHandWrittenItems(t *testing.T) {
	env := fixtureHome(t)
	write(t, handRule(env), handWrittenRule)

	firstRun(t, env)

	if got := read(t, handRule(env)); !strings.Contains(got, "  id: ps_") || strings.Contains(got, "ps_tmp_") {
		t.Errorf("item after the first run:\n%s", got)
	}
	if !strings.Contains(read(t, claudeMD(env)), handWrittenRule) {
		t.Error("the hand written rule was not applied")
	}
}

func TestAcknowledgementOfHandWrittenItemSurvivesSync(t *testing.T) {
	env := fixtureHome(t)
	firstRun(t, env)
	permission := filepath.Join(env.LibraryDir(), "permissions", "secrets.yaml")
	write(t, permission, "deny_paths:\n  - ~/.ssh/**\n")
	if len(findings(checkReport(t, env), drift.KindAckRequired)) == 0 {
		t.Fatal("fixture needs an acknowledgement")
	}

	run(t, env, &script{answers: []answer{{acknowledge, "yes"}, {syncNow, "yes"}}}, doctor.Options{})

	saved, err := policy.LoadAcks(env.AcksPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.List) == 0 {
		t.Fatal("no acknowledgement saved")
	}
	for _, ack := range saved.List {
		if strings.HasPrefix(string(ack.ItemID), "ps_tmp_") || !strings.Contains(read(t, permission), string(ack.ItemID)) {
			t.Errorf("acknowledgement names %s, the item is:\n%s", ack.ItemID, read(t, permission))
		}
	}
	if left := findings(checkReport(t, env), drift.KindAckRequired); len(left) > 0 {
		t.Errorf("still to acknowledge: %v", left)
	}
}
