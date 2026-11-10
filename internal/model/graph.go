// Package model defines the infrastructure graph shared by every parser,
// renderer, and the diff engine. All slices are kept in a deterministic order
// so that equal inputs render byte-identical output.
package model

import (
	"errors"
	"fmt"
)

// SchemaVersion is the version of the public JSON shape. It is not the
// product version and only changes when the JSON schema changes.
const SchemaVersion = 1

// Group kinds. A group maps to a Compose project, a Kubernetes namespace, or a
// Terraform module.
const (
	GroupProject   = "project"
	GroupNamespace = "namespace"
	GroupModule    = "module"
)

// ValidGroupKind reports whether kind is one of the defined group kinds.
func ValidGroupKind(kind string) bool {
	switch kind {
	case GroupProject, GroupNamespace, GroupModule:
		return true
	default:
		return false
	}
}

// Group is a named collection of nodes, such as a namespace.
type Group struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Status   Status            `json:"status"`
}

// Counts holds how many elements are in each status.
type Counts struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

// Summary holds per-element counts plus the human-readable summary line.
type Summary struct {
	Nodes Counts `json:"nodes"`
	Edges Counts `json:"edges"`
	Text  string `json:"text"`
}

// Graph is the complete infrastructure model for one input.
type Graph struct {
	SchemaVersion int     `json:"schema_version"`
	Nodes         []Node  `json:"nodes"`
	Edges         []Edge  `json:"edges"`
	Groups        []Group `json:"groups"`
	Summary       Summary `json:"summary"`
}

// New returns an empty graph with initialized slices and the current schema
// version.
func New() *Graph {
	return &Graph{
		SchemaVersion: SchemaVersion,
		Nodes:         []Node{},
		Edges:         []Edge{},
		Groups:        []Group{},
	}
}

// Node returns the node with the given ID.
func (g *Graph) Node(id string) (*Node, bool) {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i], true
		}
	}
	return nil, false
}

// Validate checks structural invariants and reports every problem it finds.
func (g *Graph) Validate() error {
	var problems []error

	if g.SchemaVersion != SchemaVersion {
		problems = append(problems, fmt.Errorf(
			"model: schema_version is %d, want %d", g.SchemaVersion, SchemaVersion))
	}

	groupIDs := make(map[string]bool, len(g.Groups))
	for i, group := range g.Groups {
		label := fmt.Sprintf("groups[%d]", i)
		if group.ID != "" {
			label = fmt.Sprintf("group %q", group.ID)
		}
		switch {
		case group.ID == "":
			problems = append(problems, fmt.Errorf("model: %s: id is required", label))
		case groupIDs[group.ID]:
			problems = append(problems, fmt.Errorf("model: duplicate group id %q", group.ID))
		}
		if group.Name == "" {
			problems = append(problems, fmt.Errorf("model: %s: name is required", label))
		}
		if !ValidGroupKind(group.Kind) {
			problems = append(problems, fmt.Errorf(
				"model: %s: group kind %q is not one of project, namespace, module", label, group.Kind))
		}
		if !group.Status.Valid() {
			problems = append(problems, fmt.Errorf(
				"model: %s: status %q is not one of unchanged, added, removed, changed", label, group.Status))
		}
		groupIDs[group.ID] = true
	}

	nodeIDs := make(map[string]bool, len(g.Nodes))
	for i, node := range g.Nodes {
		label := fmt.Sprintf("nodes[%d]", i)
		if node.ID != "" {
			label = fmt.Sprintf("node %q", node.ID)
		}
		switch {
		case node.ID == "":
			problems = append(problems, fmt.Errorf("model: %s: id is required", label))
		case nodeIDs[node.ID]:
			problems = append(problems, fmt.Errorf("model: duplicate node id %q", node.ID))
		}
		if node.Name == "" {
			problems = append(problems, fmt.Errorf("model: %s: name is required", label))
		}
		if !node.Type.Valid() {
			problems = append(problems, fmt.Errorf(
				"model: %s: type %q is not a supported node type", label, node.Type))
		}
		if !node.Status.Valid() {
			problems = append(problems, fmt.Errorf(
				"model: %s: status %q is not one of unchanged, added, removed, changed", label, node.Status))
		}
		if node.Source.File == "" {
			problems = append(problems, fmt.Errorf(
				"model: %s: source.file is required; set the relative path of the defining file", label))
		}
		if node.Metadata == nil {
			problems = append(problems, fmt.Errorf(
				"model: %s: metadata is null; use an empty map so JSON renders as {}", label))
		}
		if node.Group != "" && !groupIDs[node.Group] {
			problems = append(problems, fmt.Errorf(
				"model: %s: group %q is not defined in groups", label, node.Group))
		}
		nodeIDs[node.ID] = true
	}

	type edgeKey struct{ from, to string }
	seenEdges := make(map[edgeKey]bool, len(g.Edges))
	for i, edge := range g.Edges {
		label := fmt.Sprintf("edges[%d]", i)
		if edge.From != "" && edge.To != "" {
			label = fmt.Sprintf("edge %s -> %s", edge.From, edge.To)
		}
		if !nodeIDs[edge.From] {
			problems = append(problems, fmt.Errorf(
				"model: %s: from node %q is not defined", label, edge.From))
		}
		if !nodeIDs[edge.To] {
			problems = append(problems, fmt.Errorf(
				"model: %s: to node %q is not defined", label, edge.To))
		}
		if edge.From != "" && edge.From == edge.To {
			problems = append(problems, fmt.Errorf(
				"model: %s: a node cannot point at itself", label))
		}
		if !edge.Kind.Valid() {
			problems = append(problems, fmt.Errorf(
				"model: %s: kind %q is not a supported edge kind", label, edge.Kind))
		}
		if !edge.Status.Valid() {
			problems = append(problems, fmt.Errorf(
				"model: %s: status %q is not one of unchanged, added, removed, changed", label, edge.Status))
		}
		key := edgeKey{edge.From, edge.To}
		if seenEdges[key] {
			problems = append(problems, fmt.Errorf(
				"model: %s: duplicate edge; keep one edge per node pair and encode the most specific kind", label))
		}
		seenEdges[key] = true
	}

	return errors.Join(problems...)
}
