package claudecode

import (
	"regexp"
	"strings"
)

const (
	allowList        = "allow"
	denyList         = "deny"
	askList          = "ask"
	permissionsPath  = "/permissions/"
	hooksPath        = "/hooks/"
	hookCommandField = "/hooks/0/command"
)

var (
	permissionLists = []string{allowList, denyList, askList}
	bashPattern     = regexp.MustCompile(`^Bash\((.+?)(?: \*|:\*)\)$`)
	serverNameChars = strings.NewReplacer(".", "_", " ", "_")
)

func bashRule(command string) string { return "Bash(" + command + " *)" }

func readRule(path string) string { return "Read(" + claudePath(path) + ")" }

func editRule(path string) string { return "Edit(" + claudePath(path) + ")" }

func mcpRule(server string) string { return "mcp__" + serverNameChars.Replace(server) }

func claudePath(path string) string {
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		return "/" + path
	}
	return path
}

func portablePath(path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "//"):
		return path[1:], true
	case strings.HasPrefix(path, "/"):
		return "", false
	default:
		return path, true
	}
}

func ruleArgument(rule, tool string) (string, bool) {
	inner, found := strings.CutPrefix(rule, tool+"(")
	if !found || !strings.HasSuffix(inner, ")") {
		return "", false
	}
	return strings.TrimSuffix(inner, ")"), true
}
