package render

import (
	"fmt"
	"strings"

	"github.com/infradiff/infradiff/internal/model"
)

// Mermaid renders a flowchart. Colors are applied with classDef for nodes and
// linkStyle for edges so added, removed, and changed elements stand out.
func Mermaid(graph *model.Graph) []byte {
	g := prepared(graph)
	nodeIDs := mermaidNodeIDs(g.Nodes)

	var b strings.Builder
	b.WriteString("flowchart LR\n")
	fmt.Fprintf(&b, "  %%%% %s\n", g.Summary.Text)

	nodesByGroup := make(map[string][]model.Node, len(g.Groups))
	for _, node := range g.Nodes {
		nodesByGroup[node.Group] = append(nodesByGroup[node.Group], node)
	}

	for _, group := range g.Groups {
		nodes := nodesByGroup[group.ID]
		if len(nodes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  subgraph %s[\"%s\"]\n",
			"g_"+sanitizeID(group.ID), mermaidLabel(group.Name, group.Kind))
		for _, node := range nodes {
			writeMermaidNode(&b, "    ", node, nodeIDs)
		}
		b.WriteString("  end\n")
	}
	for _, node := range nodesByGroup[""] {
		writeMermaidNode(&b, "  ", node, nodeIDs)
	}

	for _, edge := range g.Edges {
		fmt.Fprintf(&b, "  %s -->|%s| %s\n",
			nodeIDs[edge.From], edge.Kind, nodeIDs[edge.To])
	}

	b.WriteString("  classDef unchanged fill:#f8fafc,stroke:#94a3b8,color:#0f172a\n")
	b.WriteString("  classDef added fill:#dcfce7,stroke:#16a34a,color:#14532d\n")
	b.WriteString("  classDef removed fill:#fee2e2,stroke:#dc2626,color:#7f1d1d\n")
	b.WriteString("  classDef changed fill:#fef9c3,stroke:#ca8a04,color:#713f12\n")
	for _, node := range g.Nodes {
		fmt.Fprintf(&b, "  class %s %s\n", nodeIDs[node.ID], node.Status)
	}

	for i, edge := range g.Edges {
		fmt.Fprintf(&b, "  linkStyle %d %s\n", i, mermaidEdgeStyle(edge.Status))
	}

	return []byte(b.String())
}

func writeMermaidNode(b *strings.Builder, indent string, node model.Node, ids map[string]string) {
	fmt.Fprintf(b, "%s%s\n", indent,
		mermaidShape(ids[node.ID], mermaidLabel(node.Name, string(node.Type)), node.Type))
}

// mermaidNodeIDs assigns Mermaid-safe, collision-free IDs in sorted node order.
func mermaidNodeIDs(nodes []model.Node) map[string]string {
	ids := make(map[string]string, len(nodes))
	used := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		base := "n_" + sanitizeID(node.ID)
		candidate := base
		for suffix := 2; used[candidate]; suffix++ {
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		used[candidate] = true
		ids[node.ID] = candidate
	}
	return ids
}

func sanitizeID(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
}

// mermaidLabel keeps quotes and markup out of labels, which would otherwise
// break the diagram for names read from untrusted files.
func mermaidLabel(text, suffix string) string {
	replacer := strings.NewReplacer(
		"&", "and",
		"<", "(",
		">", ")",
		"\"", "'",
		"\n", " ",
		"\r", " ",
	)
	label := replacer.Replace(text)
	if suffix == "" {
		return label
	}
	return label + "<br/>" + replacer.Replace(suffix)
}

func mermaidShape(id, label string, nodeType model.NodeType) string {
	quoted := "\"" + label + "\""
	switch nodeType {
	case model.NodeDatabase, model.NodeStorage:
		return fmt.Sprintf("%s[(%s)]", id, quoted)
	case model.NodeCache, model.NodeFunction:
		return fmt.Sprintf("%s([%s])", id, quoted)
	case model.NodeQueue:
		return fmt.Sprintf("%s[[%s]]", id, quoted)
	case model.NodeLoadBalancer, model.NodeIngress:
		return fmt.Sprintf("%s{{%s}}", id, quoted)
	case model.NodeNetwork:
		return fmt.Sprintf("%s((%s))", id, quoted)
	default:
		return fmt.Sprintf("%s[%s]", id, quoted)
	}
}

func mermaidEdgeStyle(status model.Status) string {
	switch status {
	case model.StatusAdded:
		return "stroke:#16a34a,stroke-width:2px"
	case model.StatusRemoved:
		return "stroke:#dc2626,stroke-width:2px,stroke-dasharray:5 3"
	case model.StatusChanged:
		return "stroke:#ca8a04,stroke-width:2px"
	default:
		return "stroke:#94a3b8,stroke-width:1px"
	}
}
