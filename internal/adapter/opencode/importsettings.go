package opencode

import (
	"maps"
	"slices"
	"strings"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/manifest"
)

const (
	permissionName   = "opencode permissions"
	legacyPermission = "permission"
	legacyTools      = "tools"
	shellAction      = "shell"
	legacyShell      = "bash"
	readAction       = "read"
	editAction       = "edit"
	subagentAction   = "subagent"
	anyResource      = "*"

	noteDenyRule  = "deny rule %s %q is not portable; not imported"
	noteAskRules  = "ask rules are not imported"
	noteDeadShell = "permissions rule bash %q never matches, because opencode V2 calls the action shell; not imported"
	noteRuleShape = "a permission rule has an unexpected shape; not imported"
	noteRuleKey   = "allow rule %s %q: %v"
)

type foundRule struct {
	permissionRule
	path, key string
}

var legacyActions = map[string]string{
	"write": editAction, "patch": editAction, "apply_patch": editAction,
	"task": subagentAction, legacyShell: shellAction,
}

func normalAction(action string) string {
	if renamed, ok := legacyActions[action]; ok {
		return renamed
	}
	return action
}

func (im *importer) permissions(documents []document) {
	var rules []foundRule
	for _, doc := range documents {
		rules = append(rules, im.toolRules(doc)...)
		rules = append(rules, im.legacyRules(doc)...)
		rules = append(rules, im.nativeRules(doc)...)
	}
	denied := map[permissionRule]bool{}
	for _, rule := range rules {
		if rule.Effect == effectDeny {
			denied[rule.permissionRule] = true
		}
	}
	portable := &library.PortablePermission{}
	source := ""
	asked := map[string]bool{}
	carry := func(list *[]string, value, path string) {
		if source == "" {
			source = path
		}
		if !slices.Contains(*list, value) {
			*list = append(*list, value)
		}
	}
	for _, rule := range rules {
		paired := func(action string) bool {
			return denied[permissionRule{Action: action, Resource: rule.Resource, Effect: effectDeny}]
		}
		switch {
		case rule.Effect == effectAllow:
			value := rule.Action + " " + rule.Resource
			im.out.Grants = append(im.out.Grants, adapter.Grant{Tool: tool, Path: rule.path, Key: rule.key, Value: value})
			continue
		case rule.Effect == effectAsk:
			if !asked[rule.path] {
				im.note(rule.path, noteAskRules)
				asked[rule.path] = true
			}
			continue
		case rule.Action == shellAction:
			if command, ok := portableCommand(rule.Resource); ok {
				carry(&portable.DenyCommands, command, rule.path)
				continue
			}
		case rule.Action == readAction && paired(editAction) && expressiblePath(rule.Resource):
			carry(&portable.DenyPaths, rule.Resource, rule.path)
			continue
		case rule.Action == editAction && paired(readAction) && expressiblePath(rule.Resource):
			continue
		}
		im.note(rule.path, noteDenyRule, rule.Action, rule.Resource)
	}
	if source != "" {
		im.out.Items = append(im.out.Items, library.Item{Kind: library.KindPermission, Name: permissionName, Source: source, Portable: portable})
	}
}

func portableCommand(resource string) (string, bool) {
	trimmed, prefixed := strings.CutSuffix(resource, " "+anyResource)
	command, ok := commandPrefix(trimmed)
	return trimmed, prefixed && ok && command == trimmed
}

func (im *importer) toolRules(doc document) []foundRule {
	var rules []foundRule
	tools := object(doc.fields[legacyTools])
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		enabled, ok := tools[name].(bool)
		if !ok {
			im.note(doc.path, noteRuleShape)
			continue
		}
		effect := effectDeny
		if enabled {
			effect = effectAllow
		}
		rules = append(rules, foundRule{
			permissionRule: permissionRule{Action: normalAction(name), Resource: anyResource, Effect: effect},
			path:           doc.path,
			key:            "/" + legacyTools + "/" + escapePointer(name),
		})
	}
	return rules
}

func (im *importer) legacyRules(doc document) []foundRule {
	value, present := doc.fields[legacyPermission]
	if !present {
		return nil
	}
	var rules []foundRule
	add := func(action, resource string, effect any, key string) {
		if !slices.Contains(effects, text(effect)) {
			im.note(doc.path, noteRuleShape)
			return
		}
		rules = append(rules, foundRule{
			permissionRule: permissionRule{Action: normalAction(action), Resource: resource, Effect: text(effect)},
			path:           doc.path,
			key:            key,
		})
	}
	pointer := "/" + legacyPermission
	actions, isObject := value.(map[string]any)
	if !isObject {
		add(anyResource, anyResource, value, pointer)
		return rules
	}
	for _, action := range slices.Sorted(maps.Keys(actions)) {
		actionPointer := pointer + "/" + escapePointer(action)
		resources, isMap := actions[action].(map[string]any)
		if !isMap {
			add(action, anyResource, actions[action], actionPointer)
			continue
		}
		for _, resource := range slices.Sorted(maps.Keys(resources)) {
			add(action, resource, resources[resource], actionPointer+"/"+escapePointer(resource))
		}
	}
	return rules
}

func (im *importer) nativeRules(doc document) []foundRule {
	value, present := doc.fields[permissionsKey]
	if !present {
		return nil
	}
	entries, ok := value.([]any)
	if !ok {
		im.note(doc.path, noteRuleShape)
		return nil
	}
	var rules []foundRule
	for _, entry := range entries {
		rule, ok := ruleOf(entry)
		if !ok {
			im.note(doc.path, noteRuleShape)
			continue
		}
		if rule.Action == legacyShell {
			im.note(doc.path, noteDeadShell, rule.Resource)
			continue
		}
		key, err := ruleKey(entry)
		if err != nil {
			im.note(doc.path, noteRuleKey, rule.Action, rule.Resource, err)
			continue
		}
		rules = append(rules, foundRule{permissionRule: rule, path: doc.path, key: key})
	}
	return rules
}

func ruleKey(value any) (string, error) {
	content, err := marshal(value)
	if err != nil {
		return "", err
	}
	hash, err := manifest.Hash(manifest.UnitJSONElement, content)
	if err != nil {
		return "", err
	}
	return "/" + permissionsKey + "@#" + hash, nil
}
