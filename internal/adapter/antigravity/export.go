package antigravity

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/antonL95/promptstor/internal/adapter"
	"github.com/antonL95/promptstor/internal/backup"
	"github.com/antonL95/promptstor/internal/library"
	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/plan"
)

const (
	fileMode             = fs.FileMode(0o644)
	frontmatterFence     = "---\n"
	reasonNoFileName     = "the name has no characters usable in a file name"
	reasonCredentialPath = "%s matches the credential exclusion list"
)

type exporter struct {
	t           adapter.Target
	l           layout
	units       []plan.Unit
	unsupported []adapter.Unsupported
}

func (Adapter) Export(t adapter.Target, items []library.Item) ([]plan.Unit, []adapter.Unsupported, error) {
	l, ok := layoutFor(t)
	if !ok {
		return nil, nil, nil
	}
	ex := &exporter{t: t, l: l}
	ex.rules(items)
	hooks, err := readObject(l.in(hooksName))
	if err != nil {
		return nil, nil, err
	}
	config, err := readObject(l.in(mcpName))
	if err != nil {
		return nil, nil, err
	}
	servers := rawObject(config.fields[serversKey])
	for _, it := range items {
		switch it.Kind {
		case library.KindSkill:
			ex.skill(it, it)
		case library.KindCommand:
			ex.command(it)
		case library.KindSubagent:
			ex.subagent(it)
		case library.KindPermission:
			ex.permission(it)
		case library.KindHook:
			ex.hook(it, hooks.fields)
		case library.KindMCP:
			ex.server(it, servers)
		}
	}
	slices.SortStableFunc(ex.units, func(a, b plan.Unit) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
	})
	return ex.units, ex.unsupported, nil
}

func (ex *exporter) report(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason})
}

func (ex *exporter) block(it library.Item, reason string) {
	ex.unsupported = append(ex.unsupported, adapter.Unsupported{ItemID: it.ID, Name: it.Name, Tool: tool, Reason: reason, Blocking: true})
}

func (ex *exporter) add(u plan.Unit) {
	u.Tool = tool
	u.Root = ex.l.root
	if u.Mode == 0 {
		u.Mode = fileMode
	}
	ex.units = append(ex.units, u)
}

func (ex *exporter) document(it library.Item, path string, content []byte) bool {
	if backup.Excluded(ex.t.Env, path) {
		ex.report(it, fmt.Sprintf(reasonCredentialPath, filepath.Base(path)))
		return false
	}
	ex.add(plan.Unit{Path: path, Kind: manifest.UnitFile, Content: content, Items: []library.ID{it.ID}})
	return true
}

type header struct {
	node yaml.Node
	err  error
}

func newHeader() *header {
	return &header{node: yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

func (h *header) set(key string, value any) {
	keyNode, valueNode := &yaml.Node{}, &yaml.Node{}
	keyNode.SetString(key)
	if err := valueNode.Encode(value); err != nil {
		h.err = cmp.Or(h.err, fmt.Errorf("frontmatter %s: %w", key, err))
		return
	}
	h.node.Content = append(h.node.Content, keyNode, valueNode)
}

func (h *header) markdown(body string) ([]byte, error) {
	if h.err != nil {
		return nil, h.err
	}
	var buffer bytes.Buffer
	buffer.WriteString(frontmatterFence)
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&h.node); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	buffer.WriteString(frontmatterFence)
	buffer.WriteString(body)
	return managed.WithNotice(buffer.Bytes()), nil
}

type jsonObject struct{ fields map[string]json.RawMessage }

func readObject(path string) (jsonObject, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return jsonObject{}, nil
	}
	if err != nil {
		return jsonObject{}, fmt.Errorf("read %s: %w", path, err)
	}
	fields, _ := decodeObject(data)
	return jsonObject{fields: fields}, nil
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	return fields
}

func marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
