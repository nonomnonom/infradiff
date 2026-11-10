// Package risk finds notable configuration issues in infrastructure graphs.
package risk

import (
	"sort"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
)

// Risk is one configuration issue attached to a graph node.
type Risk struct {
	Severity string `json:"severity"`
	Node     string `json:"node"`
	Message  string `json:"message"`
}

var severityRank = map[string]int{"high": 0, "medium": 1, "low": 2}

// Graph reports risks in one graph. A removed data store is a data-loss risk;
// other removed nodes no longer expose anything, so their exposure rules are
// skipped.
func Graph(graph *model.Graph) []Risk {
	out := []Risk{}
	for _, node := range graph.Nodes {
		if node.Status == model.StatusRemoved {
			if item, ok := removedDataRisk(node); ok {
				out = append(out, item)
			}
			continue
		}
		out = append(out, nodeRisks(node)...)
	}
	return sortRisks(out)
}

// Changes reports risks introduced by the new side of a diff, plus data-loss
// risks for removed data stores.
func Changes(graph *model.Graph) []Risk {
	out := []Risk{}
	for _, node := range graph.Nodes {
		switch node.Status {
		case model.StatusRemoved:
			if item, ok := removedDataRisk(node); ok {
				out = append(out, item)
			}
		case model.StatusAdded, model.StatusChanged:
			out = append(out, nodeRisks(node)...)
		}
	}
	return sortRisks(out)
}

func removedDataRisk(node model.Node) (Risk, bool) {
	if !isDataStore(node.Type) {
		return Risk{}, false
	}
	severity := "high"
	if node.Type == model.NodeCache || node.Type == model.NodeQueue {
		severity = "medium"
	}
	return Risk{
		Severity: severity,
		Node:     node.ID,
		Message:  "will be deleted; confirm backups and that data loss is intended",
	}, true
}

func nodeRisks(node model.Node) []Risk {
	metadata := node.Metadata
	out := []Risk{}

	if isDataStore(node.Type) {
		if metadata["ports"] != "" {
			out = append(out, Risk{
				Severity: "high",
				Node:     node.ID,
				Message:  "publishes host ports; keep data stores on a private network",
			})
		}
		if strings.EqualFold(metadata["publicly_accessible"], "true") {
			out = append(out, Risk{
				Severity: "high",
				Node:     node.ID,
				Message:  "is publicly accessible; restrict access to the private network",
			})
		}
	}

	if node.Type == model.NodeLoadBalancer {
		if kind := metadata["type"]; kind == "LoadBalancer" || kind == "NodePort" {
			out = append(out, Risk{
				Severity: "medium",
				Node:     node.ID,
				Message:  "is reachable from outside the cluster",
			})
		}
	}

	if image, ok := metadata["image"]; ok && !imagePinned(image) {
		out = append(out, Risk{
			Severity: "medium",
			Node:     node.ID,
			Message:  "uses an unpinned image tag; pin a version or digest",
		})
	}

	if publicCIDR(metadata) {
		out = append(out, Risk{
			Severity: "high",
			Node:     node.ID,
			Message:  "allows traffic from 0.0.0.0/0 or ::/0; restrict the source range",
		})
	}

	return out
}

func publicCIDR(metadata map[string]string) bool {
	for key, value := range metadata {
		if key != "cidrs" && key != "cidr_blocks" && !strings.HasSuffix(key, "_cidrs") {
			continue
		}
		for _, cidr := range strings.Split(value, ",") {
			switch strings.TrimSpace(cidr) {
			case "0.0.0.0/0", "::/0":
				return true
			}
		}
	}
	return false
}

func isDataStore(nodeType model.NodeType) bool {
	switch nodeType {
	case model.NodeDatabase, model.NodeCache, model.NodeQueue, model.NodeStorage:
		return true
	default:
		return false
	}
}

func sortRisks(out []Risk) []Risk {
	sort.Slice(out, func(i, j int) bool {
		if severityRank[out[i].Severity] != severityRank[out[j].Severity] {
			return severityRank[out[i].Severity] < severityRank[out[j].Severity]
		}
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// imagePinned reports whether an image reference names a version or digest
// rather than floating on latest.
func imagePinned(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return false
	}
	tag := image[lastColon+1:]
	return tag != "" && tag != "latest"
}
