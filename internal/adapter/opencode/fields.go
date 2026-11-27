package opencode

import (
	"regexp"
	"slices"
)

const (
	nameKey        = "name"
	descriptionKey = "description"
	modeKey        = "mode"
	modelKey       = "model"
	variantKey     = "variant"
	permissionsKey = "permissions"
	disabledKey    = "disabled"
	subagentKey    = "subagent"
	subtaskKey     = "subtask"
	stepsKey       = "steps"
	modeMeta       = "opencode_mode"
	subagentMode   = "subagent"
	primaryMode    = "primary"
	effectAllow    = "allow"
	effectAsk      = "ask"
	effectDeny     = "deny"
)

var (
	modelReference = regexp.MustCompile(`^[^/#]+/[^#]+(#[^#]+)?$`)
	hexColor       = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	builtinAgents  = []string{"build", "compaction", "explore", "general", "plan", "summary", "title"}
	agentModes     = []string{subagentMode, primaryMode, "all"}
	effects        = []string{effectAllow, effectAsk, effectDeny}
)

type field struct {
	meta, name string
	accepts    func(any) bool
}

var agentFields = []field{
	{modeMeta, modeKey, isMode},
	{"opencode_model", modelKey, isModel},
	{"opencode_hidden", "hidden", isBool},
	{"opencode_color", "color", isColor},
	{"opencode_steps", stepsKey, isPositiveInteger},
	{"opencode_disabled", disabledKey, isBool},
	{"opencode_permissions", permissionsKey, isRuleset},
}

var commandFields = []field{
	{"opencode_agent", "agent", isText},
	{"opencode_model", modelKey, isModel},
	{"opencode_subagent", subagentKey, isBool},
}

func fieldNamed(fields []field, name string) (field, bool) {
	at := slices.IndexFunc(fields, func(f field) bool { return f.name == name })
	if at < 0 {
		return field{}, false
	}
	return fields[at], true
}

func isText(value any) bool {
	_, ok := value.(string)
	return ok
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

func isMode(value any) bool {
	return slices.Contains(agentModes, text(value))
}

func isModel(value any) bool { return modelReference.MatchString(text(value)) }

func isColor(value any) bool { return hexColor.MatchString(text(value)) }

func isPositiveInteger(value any) bool {
	number, ok := value.(int)
	return ok && number > 0
}

type permissionRule struct {
	Action   string `json:"action" yaml:"action"`
	Resource string `json:"resource" yaml:"resource"`
	Effect   string `json:"effect" yaml:"effect"`
}

func ruleOf(value any) (permissionRule, bool) {
	fields := object(value)
	action, hasAction := fields["action"].(string)
	resource, hasResource := fields["resource"].(string)
	effect := text(fields["effect"])
	rule := permissionRule{Action: action, Resource: resource, Effect: effect}
	return rule, hasAction && hasResource && len(fields) == 3 && slices.Contains(effects, effect)
}

func rulesetOf(value any) ([]permissionRule, bool) {
	entries, ok := value.([]any)
	if !ok {
		return nil, false
	}
	rules := make([]permissionRule, 0, len(entries))
	for _, entry := range entries {
		rule, ok := ruleOf(entry)
		if !ok {
			return nil, false
		}
		rules = append(rules, rule)
	}
	return rules, true
}

func isRuleset(value any) bool {
	_, ok := rulesetOf(value)
	return ok
}
