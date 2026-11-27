package backup

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/paths"
)

var credentialBasenames = []string{
	"auth.json",
	".credentials.json",
	"oauth_creds.json",
	"mcp-oauth-tokens-v2.json",
	"mcp_oauth_tokens.json",
	"mcp-auth.json",
	"google_accounts.json",
}

var credentialName = regexp.MustCompile(`(?i)(token|credential|secret|oauth)`)

var claudeStatePointers = []string{"/customApiKeyResponses", "/oauthAccount", "/primaryApiKey"}

func Excluded(env paths.Env, path string) bool {
	path = filepath.Clean(path)
	if slices.Contains(credentialBasenames, filepath.Base(path)) {
		return true
	}
	if slices.Contains(excludedFiles(env), path) {
		return true
	}
	if slices.ContainsFunc(excludedTrees(env), func(root string) bool { return within(root, path) }) {
		return true
	}
	return credentialNamed(path)
}

func credentialNamed(path string) bool {
	name := filepath.Base(path)
	if strings.EqualFold(filepath.Ext(name), ".md") || !credentialName.MatchString(name) {
		return false
	}
	info, err := os.Lstat(path)
	return err != nil || !info.IsDir()
}

func ExcludedPointers(env paths.Env, path string) []string {
	if filepath.Clean(path) != filepath.Join(env.Home, ".claude.json") {
		return nil
	}
	return slices.Clone(claudeStatePointers)
}

func excludedFiles(env paths.Env) []string {
	files := []string{filepath.Join(env.Home, ".config", "opencode", "service.json")}
	if env.ConfigHome != "" {
		files = append(files, filepath.Join(env.ConfigHome, "opencode", "service.json"))
	}
	return files
}

func excludedTrees(env paths.Env) []string {
	return []string{
		filepath.Join(env.Home, ".local", "share", "opencode"),
		filepath.Join(env.Home, ".claude", "backups"),
	}
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}

func pointerExcluded(excluded []string, key string) bool {
	return slices.ContainsFunc(excluded, func(pointer string) bool {
		return covers(pointer, key) || covers(key, pointer)
	})
}

func covers(parent, key string) bool {
	return parent == "" || key == parent || strings.HasPrefix(key, parent+"/") || strings.HasPrefix(key, parent+"@")
}
