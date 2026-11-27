package antigravity

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
)

const (
	nameKey               = "name"
	reasonDropped         = "written without %s, which Antigravity %s files do not take"
	reasonSkillSummary    = "Antigravity does not load a skill without a description"
	reasonProjectCommands = "Antigravity commands become skills, and .agents/skills is shared with other tools"
	reasonNoPrompt        = "the command has no prompt"
	reasonAgentSummary    = "Antigravity needs a description for a subagent"
	reasonBuiltinAgent    = "%q is a built in Antigravity agent name"
	reasonAgentFolder     = "agents/%s/agent.md already defines this agent; not written"
)

var (
	agentName       = regexp.MustCompile(`^[a-z0-9_-]+$`)
	builtinAgents   = []string{"research", "browser", "self"}
	modelTiers      = []string{"inherit", "flash", "pro"}
	commandPolicies = []string{"off", "auto", "eager", "sandbox"}
)

type agentField struct {
	meta, name string
	accepts    func(any) bool
}

var agentFields = []agentField{
	{"antigravity_tools", "tools", isTextList},
	{"antigravity_main_agent", "mainAgent", isBool},
	{"antigravity_subagent", "subagent", isBool},
	{"antigravity_model", "model", isOneOf(modelTiers)},
	{"antigravity_command_execution_policy", "commandExecutionPolicy", isOneOf(commandPolicies)},
	{"antigravity_skills", "skills", isTextList},
	{"antigravity_plugins", "plugins", isTextList},
}

func agentFieldNamed(name string) (agentField, bool) {
	at := slices.IndexFunc(agentFields, func(f agentField) bool { return f.name == name })
	if at < 0 {
		return agentField{}, false
	}
	return agentFields[at], true
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

func isText(value any) bool {
	_, ok := value.(string)
	return ok
}

func isTextList(value any) bool {
	entries, ok := value.([]any)
	return ok && !slices.ContainsFunc(entries, func(entry any) bool { return !isText(entry) })
}

func isOneOf(allowed []string) func(any) bool {
	return func(value any) bool {
		text, ok := value.(string)
		return ok && slices.Contains(allowed, text)
	}
}

func (ex *exporter) skill(it, as library.Item) {
	units, err := adapter.SkillUnits(ex.t, tool, as)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	home := ex.l.in(skillsDir, as.Name)
	for _, u := range units {
		if backup.Excluded(ex.t.Env, u.Path) {
			relative, _ := filepath.Rel(home, u.Path)
			ex.report(it, fmt.Sprintf(reasonCredentialPath, "skill file "+filepath.ToSlash(relative)))
			return
		}
	}
	if strings.TrimSpace(as.Description) == "" {
		ex.report(it, reasonSkillSummary)
		return
	}
	ex.units = append(ex.units, units...)
}

func (ex *exporter) command(it library.Item) {
	slug := library.Slug(it.Name)
	switch {
	case ex.l.scope != adapter.ScopeUser:
		ex.report(it, reasonProjectCommands)
		return
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case strings.TrimSpace(it.Body) == "":
		ex.report(it, reasonNoPrompt)
		return
	}
	asSkill := library.Item{ID: it.ID, Kind: library.KindSkill, Name: slug, Description: it.Description, Body: it.Body}
	written := len(ex.units)
	ex.skill(it, asSkill)
	if len(ex.units) == written {
		return
	}
	dropped := slices.DeleteFunc(it.MetaKeys(), func(key string) bool { return key == nameKey || key == descriptionKey })
	ex.reportDropped(it, dropped, "skill")
}

func (ex *exporter) subagent(it library.Item) {
	slug := library.Slug(it.Name)
	name := it.Name
	if !agentName.MatchString(name) {
		name = slug
	}
	switch {
	case slug == "":
		ex.report(it, reasonNoFileName)
		return
	case strings.TrimSpace(it.Description) == "":
		ex.report(it, reasonAgentSummary)
		return
	case slices.Contains(builtinAgents, name):
		ex.report(it, fmt.Sprintf(reasonBuiltinAgent, name))
		return
	case exists(ex.l.in(agentsDir, slug, agentFile)):
		ex.report(it, fmt.Sprintf(reasonAgentFolder, slug))
		return
	}
	frontmatter := newHeader()
	frontmatter.set(nameKey, name)
	frontmatter.set(descriptionKey, it.Description)
	written := []string{nameKey, descriptionKey}
	for _, field := range agentFields {
		if value, present := it.Meta[field.meta]; present && field.accepts(value) {
			frontmatter.set(field.name, value)
			written = append(written, field.meta)
		}
	}
	content, err := frontmatter.markdown(it.Body)
	if err != nil {
		ex.report(it, err.Error())
		return
	}
	if !ex.document(it, ex.l.in(agentsDir, slug+markdownExtension), content) {
		return
	}
	dropped := slices.DeleteFunc(it.MetaKeys(), func(key string) bool { return slices.Contains(written, key) })
	ex.reportDropped(it, dropped, "agent")
}

func (ex *exporter) reportDropped(it library.Item, keys []string, kind string) {
	if len(keys) == 0 {
		return
	}
	slices.Sort(keys)
	ex.report(it, fmt.Sprintf(reasonDropped, strings.Join(keys, ", "), kind))
}
