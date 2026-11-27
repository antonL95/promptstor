package library

import "fmt"

func ParseMarkdown(kind Kind, name string, data []byte) (Item, error) {
	l, err := layoutOf(kind)
	if err != nil {
		return Item{}, err
	}
	if l.isYAML() {
		return Item{}, fmt.Errorf("%s items are not Markdown", kind)
	}
	return decodeItem(kind, name, data)
}
