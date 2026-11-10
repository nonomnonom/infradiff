package model

// Status describes how a graph element differs between two versions.
type Status string

// The four states every graph element can be in.
const (
	StatusUnchanged Status = "unchanged"
	StatusAdded     Status = "added"
	StatusRemoved   Status = "removed"
	StatusChanged   Status = "changed"
)

// Valid reports whether s is one of the defined statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusUnchanged, StatusAdded, StatusRemoved, StatusChanged:
		return true
	default:
		return false
	}
}
