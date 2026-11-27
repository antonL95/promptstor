package plan

import (
	"bytes"
	"errors"
	"slices"

	"github.com/antonL95/promptstor/internal/managed"
	"github.com/antonL95/promptstor/internal/manifest"
	"github.com/antonL95/promptstor/internal/structedit"
)

var errUnknownKind = errors.New("unknown unit kind")

type editor struct {
	change       *FileChange
	manifest     *manifest.Manifest
	adopt        bool
	doc          []byte
	exists       bool
	deleted      bool
	editedByHand bool
	record       []Unit
	adopted      []Unit
}

func (e *editor) run() string {
	for _, entry := range e.change.Removed {
		if reason := e.remove(entry); reason != "" {
			return reason
		}
	}
	for _, u := range e.change.Units {
		if reason := e.apply(u); reason != "" {
			return reason
		}
	}
	e.finish()
	return ""
}

func (e *editor) remove(entry manifest.Entry) string {
	if entry.Adopted || !e.exists || e.deleted {
		return ""
	}
	if entry.Unit == manifest.UnitFile {
		if current, err := manifest.Hash(manifest.UnitFile, e.doc); err != nil || current != entry.Hash {
			e.editedByHand = true
		}
		e.deleted = true
		e.doc = nil
		return ""
	}
	out, err := removeUnit(entry.Unit, e.doc, entry.Key)
	if err != nil {
		return err.Error()
	}
	e.doc = out
	return ""
}

func (e *editor) apply(u Unit) string {
	present := e.exists && !e.deleted
	current, found, err := ExtractUnit(u.Kind, e.doc, u.Key, present)
	if err != nil {
		return err.Error()
	}
	want, err := manifest.Hash(u.Kind, u.Content)
	if err != nil {
		return err.Error()
	}
	entry, owned := e.manifest.Get(u.Path, u.Kind, u.Key)
	if found {
		if have, err := manifest.Hash(u.Kind, current); err == nil && have == want {
			if !owned || !sameEntry(entry, u, want) {
				e.record = append(e.record, u)
			}
			if !owned || entry.Adopted {
				e.adopted = append(e.adopted, u)
			}
			return ""
		}
	}
	if u.Kind == manifest.UnitFile && present && !owned && !hasNotice(e.doc) && !e.adopt {
		return ReasonUnmanaged
	}
	out, err := setUnit(u.Kind, e.doc, u.Key, u.Content)
	if err != nil {
		return err.Error()
	}
	e.doc = out
	e.deleted = false
	return ""
}

func (e *editor) finish() {
	c := e.change
	c.After = e.doc
	c.Adopted = e.adopted
	switch {
	case e.deleted && len(c.Units) == 0:
		c.After = nil
		if e.exists {
			c.Op = OpDelete
		}
	case c.ReplaceLink:
		c.Op = OpUpdate
	case !e.exists:
		if len(c.Units) > 0 && len(e.record) < len(c.Units) {
			c.Op = OpCreate
		}
	case !bytes.Equal(c.Before, e.doc):
		c.Op = OpUpdate
	}
	if c.Op == OpNoop {
		c.Record = e.record
	}
	c.EditedByHand = e.editedByHand && c.Op == OpDelete
}

func sameEntry(entry manifest.Entry, u Unit, hash string) bool {
	return entry.Hash == hash && entry.Tool == u.Tool && slices.Equal(entry.Items, u.Items) && entry.Secret == u.Secret
}

func ExtractUnit(kind manifest.UnitKind, doc []byte, key string, present bool) ([]byte, bool, error) {
	if !present {
		return nil, false, nil
	}
	switch kind {
	case manifest.UnitFile:
		return doc, true, nil
	case manifest.UnitSection:
		return managed.Section(doc)
	case manifest.UnitJSON, manifest.UnitJSONElement:
		return structedit.JSONGet(doc, key)
	case manifest.UnitTOMLTable:
		return structedit.TOMLGetTable(doc, key)
	default:
		return nil, false, errUnknownKind
	}
}

func setUnit(kind manifest.UnitKind, doc []byte, key string, content []byte) ([]byte, error) {
	switch kind {
	case manifest.UnitFile:
		return slices.Clone(content), nil
	case manifest.UnitSection:
		return managed.SetSection(doc, content)
	case manifest.UnitJSON, manifest.UnitJSONElement:
		return structedit.JSONSet(doc, key, content)
	case manifest.UnitTOMLTable:
		return structedit.TOMLSetTable(doc, key, content)
	default:
		return nil, errUnknownKind
	}
}

func removeUnit(kind manifest.UnitKind, doc []byte, key string) ([]byte, error) {
	switch kind {
	case manifest.UnitSection:
		return managed.RemoveSection(doc)
	case manifest.UnitJSON, manifest.UnitJSONElement:
		return structedit.JSONDelete(doc, key)
	case manifest.UnitTOMLTable:
		return structedit.TOMLDeleteTable(doc, key)
	default:
		return nil, errUnknownKind
	}
}
