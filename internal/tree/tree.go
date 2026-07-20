// Package tree provides a small named tree with generic node payloads.
package tree

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrDuplicateNode = errors.New("duplicate node")
	ErrMissingParent = errors.New("missing parent")
	ErrCycle         = errors.New("tree cycle")
)

// Node stores a payload and references adjacent nodes by name.
type Node[T any] struct {
	Payload  T
	Parent   string
	Children map[string]struct{}
}

// Tree stores named nodes and their parent-child relationships.
type Tree[T any] struct {
	nodes map[string]*Node[T]
}

// New returns an empty tree.
func New[T any]() *Tree[T] {
	return &Tree[T]{nodes: make(map[string]*Node[T])}
}

// Add adds a named node. Parent references may be resolved later by Validate.
func (t *Tree[T]) Add(name, parent string, payload T) error {
	if name == "" {
		return fmt.Errorf("node name is empty")
	}
	if _, exists := t.nodes[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateNode, name)
	}
	t.nodes[name] = &Node[T]{Payload: payload, Parent: parent, Children: make(map[string]struct{})}
	return nil
}

// Node returns a named node.
func (t *Tree[T]) Node(name string) (*Node[T], bool) {
	node, ok := t.nodes[name]
	return node, ok
}

// Names returns node names in stable order.
func (t *Tree[T]) Names() []string {
	names := make([]string, 0, len(t.nodes))
	for name := range t.nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate links children and rejects missing parents and cycles.
func (t *Tree[T]) Validate() error {
	for _, node := range t.nodes {
		node.Children = make(map[string]struct{})
	}
	for name, node := range t.nodes {
		if node.Parent == "" {
			continue
		}
		parent, ok := t.nodes[node.Parent]
		if !ok {
			return fmt.Errorf("%w: %s -> %s", ErrMissingParent, name, node.Parent)
		}
		parent.Children[name] = struct{}{}
	}

	colors := make(map[string]visitState, len(t.nodes))
	for name := range t.nodes {
		if err := t.visit(name, colors); err != nil {
			return err
		}
	}
	return nil
}

// Effective returns the conjunction of enabled payloads from name to its root.
// The caller supplies the payload-to-enabled conversion so the tree stays generic.
func (t *Tree[T]) Effective(name string, enabled func(T) bool) (bool, error) {
	if enabled == nil {
		return false, fmt.Errorf("enabled function is nil")
	}
	if _, ok := t.nodes[name]; !ok {
		return false, fmt.Errorf("node not found: %s", name)
	}
	if err := t.Validate(); err != nil {
		return false, err
	}
	for current := name; current != ""; {
		node := t.nodes[current]
		if !enabled(node.Payload) {
			return false, nil
		}
		current = node.Parent
	}
	return true, nil
}

type visitState uint8

const (
	visitNone visitState = iota
	visitActive
	visitDone
)

func (t *Tree[T]) visit(name string, colors map[string]visitState) error {
	switch colors[name] {
	case visitActive:
		return fmt.Errorf("%w at %s", ErrCycle, name)
	case visitDone:
		return nil
	}
	colors[name] = visitActive
	parent := t.nodes[name].Parent
	if parent != "" {
		if err := t.visit(parent, colors); err != nil {
			return err
		}
	}
	colors[name] = visitDone
	return nil
}
