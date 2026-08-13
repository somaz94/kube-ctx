package cli

import (
	"errors"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/picker"
)

// errPickerUnavailable means no interactive terminal was available, so the
// caller should fall back to plain listing.
var errPickerUnavailable = errors.New("picker unavailable")

// newPicker builds a terminal-backed picker. It is a variable so tests can
// substitute a scripted one.
var newPicker = func(prompt string) (*picker.Picker, func() error, error) {
	p, closeTTY, err := picker.NewTTY(prompt)
	if errors.Is(err, picker.ErrNoTTY) {
		return nil, nil, errPickerUnavailable
	}
	return p, closeTTY, err
}

// pick runs the picker and returns the chosen index.
func pick(prompt string, items []picker.Item) (int, error) {
	p, closeTTY, err := newPicker(prompt)
	if err != nil {
		return 0, err
	}
	if closeTTY != nil {
		defer func() { _ = closeTTY() }()
	}
	return p.Run(items)
}

// pickContext lets the user choose a context interactively.
func pickContext(a *app, cfg *clientcmdapi.Config) (string, error) {
	list := contexts.List(cfg)
	if len(list) == 0 {
		return "", errPickerUnavailable
	}

	classifier, err := a.classifier()
	if err != nil {
		return "", err
	}

	items := make([]picker.Item, 0, len(list))
	for _, c := range list {
		item := picker.Item{Label: c.Name, Detail: c.Namespace}
		if verdict := classifier.Classify(c.Name); verdict.Label != "" {
			item.Badge = verdict.Label
			item.BadgeStyle = verdict.Style()
		}
		if c.Current {
			item.Detail = "current · " + item.Detail
		}
		items = append(items, item)
	}

	index, err := pick("context", items)
	if err != nil {
		return "", err
	}
	return list[index].Name, nil
}

// pickNamespace lets the user choose a namespace interactively.
func pickNamespace(a *app, current string, names []string) (string, error) {
	if len(names) == 0 {
		return "", errPickerUnavailable
	}

	items := make([]picker.Item, 0, len(names))
	for _, name := range names {
		item := picker.Item{Label: name}
		if name == current {
			item.Badge = "current"
		}
		items = append(items, item)
	}

	index, err := pick("namespace", items)
	if err != nil {
		return "", err
	}
	return names[index], nil
}
