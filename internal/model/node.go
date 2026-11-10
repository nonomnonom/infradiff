package model

// NodeType classifies an infrastructure component.
type NodeType string

// Node types understood by every renderer.
const (
	NodeService      NodeType = "service"
	NodeDatabase     NodeType = "database"
	NodeCache        NodeType = "cache"
	NodeQueue        NodeType = "queue"
	NodeStorage      NodeType = "storage"
	NodeLoadBalancer NodeType = "loadbalancer"
	NodeNetwork      NodeType = "network"
	NodeIngress      NodeType = "ingress"
	NodeCompute      NodeType = "compute"
	NodeFunction     NodeType = "function"
	NodeOther        NodeType = "other"
)

// Valid reports whether t is one of the defined node types.
func (t NodeType) Valid() bool {
	switch t {
	case NodeService, NodeDatabase, NodeCache, NodeQueue, NodeStorage,
		NodeLoadBalancer, NodeNetwork, NodeIngress, NodeCompute, NodeFunction,
		NodeOther:
		return true
	default:
		return false
	}
}

// Source points at the file and line a node was derived from.
//
// File is relative to the scanned root and uses forward slashes. Line is 0
// when the source format has no line information, such as Terraform plan JSON.
type Source struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Node is one infrastructure component in a graph.
type Node struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     NodeType          `json:"type"`
	Group    string            `json:"group"`
	Source   Source            `json:"source"`
	Metadata map[string]string `json:"metadata"`
	Status   Status            `json:"status"`
}

// NewNode returns a node with an initialized metadata map and unchanged status.
func NewNode(id, name string, nodeType NodeType) Node {
	return Node{
		ID:       id,
		Name:     name,
		Type:     nodeType,
		Metadata: map[string]string{},
		Status:   StatusUnchanged,
	}
}
