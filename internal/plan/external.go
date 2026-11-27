package plan

import "github.com/antonL95/promptstor/internal/fsx"

const ReasonExternal = "managed externally: "

func ExternalFor(external []string, path string) (string, bool) {
	return fsx.Within(external, path)
}

func External(external []string, path string) bool {
	_, found := fsx.Within(external, path)
	return found
}
