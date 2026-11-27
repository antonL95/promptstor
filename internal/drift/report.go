package drift

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/antonL95/promptstor/internal/library"
)

type FindingKind string

const (
	KindDiffers          FindingKind = "differs"
	KindOnlyIn           FindingKind = "only-in"
	KindMissing          FindingKind = "missing"
	KindPending          FindingKind = "pending"
	KindHandEdit         FindingKind = "hand-edit"
	KindUnsupported      FindingKind = "unsupported"
	KindAckRequired      FindingKind = "ack-required"
	KindRuntimeGrant     FindingKind = "runtime-grant"
	KindSymlinked        FindingKind = "symlinked"
	KindForeign          FindingKind = "foreign"
	KindNeedsSetup       FindingKind = "needs-setup"
	KindSharedConflict   FindingKind = "shared-conflict"
	KindLockViolation    FindingKind = "lock-violation"
	KindUnresolvedImport FindingKind = "unresolved-import"
	KindIgnoredFile      FindingKind = "ignored-file"
	KindExternal         FindingKind = "external"
	KindInfo             FindingKind = "info"
)

const secretMarker = "(secret, not shown)"

func Kinds() []FindingKind {
	return []FindingKind{
		KindDiffers, KindOnlyIn, KindMissing, KindPending, KindHandEdit, KindUnsupported,
		KindAckRequired, KindRuntimeGrant, KindSymlinked, KindForeign, KindNeedsSetup,
		KindSharedConflict, KindLockViolation, KindUnresolvedImport, KindIgnoredFile, KindExternal, KindInfo,
	}
}

func (k FindingKind) blocksClean() bool {
	switch k {
	case KindDiffers, KindMissing, KindPending, KindHandEdit, KindAckRequired, KindSharedConflict:
		return true
	default:
		return false
	}
}

type Finding struct {
	Kind   FindingKind `json:"kind"`
	Tool   string      `json:"tool,omitempty"`
	Item   library.ID  `json:"item,omitempty"`
	Name   string      `json:"name,omitempty"`
	Path   string      `json:"path,omitempty"`
	Detail string      `json:"detail,omitempty"`
}

type Report struct {
	Generated   time.Time
	LibraryHash string
	Findings    []Finding
}

func (r Report) Clean() bool {
	return !slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Kind.blocksClean() })
}

func (r Report) DriftCount() int {
	count := 0
	for _, f := range sorted(r.Findings) {
		if f.Kind.blocksClean() {
			count++
		}
	}
	return count
}

func (r Report) Fingerprint() string {
	data, err := json.Marshal(sorted(r.Findings))
	if err != nil {
		data = []byte(err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r Report) counts() map[FindingKind]int {
	counts := map[FindingKind]int{}
	for _, f := range r.Findings {
		counts[f.Kind]++
	}
	return counts
}

func sorted(findings []Finding) []Finding {
	out := slices.Clone(findings)
	slices.SortFunc(out, compareFindings)
	return slices.Compact(out)
}

func compareFindings(a, b Finding) int {
	return cmp.Or(
		cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)),
		cmp.Compare(a.Tool, b.Tool),
		cmp.Compare(a.Path, b.Path),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.Item, b.Item),
		cmp.Compare(a.Detail, b.Detail),
	)
}

func kindRank(k FindingKind) int {
	if at := slices.Index(Kinds(), k); at >= 0 {
		return at
	}
	return len(Kinds())
}

type reportJSON struct {
	Generated   time.Time           `json:"generated"`
	LibraryHash string              `json:"library_hash"`
	Clean       bool                `json:"clean"`
	Fingerprint string              `json:"fingerprint"`
	Counts      map[FindingKind]int `json:"counts"`
	Findings    []Finding           `json:"findings"`
}

func RenderJSON(w io.Writer, r Report) error {
	findings := sorted(r.Findings)
	if findings == nil {
		findings = []Finding{}
	}
	data, err := json.MarshalIndent(reportJSON{
		Generated:   r.Generated.UTC(),
		LibraryHash: r.LibraryHash,
		Clean:       r.Clean(),
		Fingerprint: r.Fingerprint(),
		Counts:      r.counts(),
		Findings:    findings,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode drift report: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write drift report: %w", err)
	}
	return nil
}
