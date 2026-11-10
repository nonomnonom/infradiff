package model

// EdgeKind describes the relationship between two nodes.
type EdgeKind string

// Edge kinds produced by the parsers. A node pair has at most one edge; when
// several relationships apply, the parser picks the most specific kind.
const (
	EdgeDependsOn EdgeKind = "depends_on"
	EdgeNetwork   EdgeKind = "network"
	EdgeVolume    EdgeKind = "volume"
	EdgePort      EdgeKind = "port"
	EdgeReference EdgeKind = "reference"
	EdgeRoutesTo  EdgeKind = "routes_to"
)

// Valid reports whether k is one of the defined edge kinds.
func (k EdgeKind) Valid() bool {
	switch k {
	case EdgeDependsOn, EdgeNetwork, EdgeVolume, EdgePort, EdgeReference, EdgeRoutesTo:
		return true
	default:
		return false
	}
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Kind   EdgeKind `json:"kind"`
	Status Status   `json:"status"`
}
