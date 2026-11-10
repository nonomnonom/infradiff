package risk

import (
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

func TestGraphFindsPublicCIDRAndDataStorePorts(t *testing.T) {
	graph := model.New()
	graph.Nodes = append(graph.Nodes,
		model.Node{
			ID: "tf:aws_security_group.web", Name: "web", Type: model.NodeNetwork,
			Source: model.Source{File: "main.tf", Line: 1},
			Metadata: map[string]string{
				"ingress_cidrs": "0.0.0.0/0",
			},
			Status: model.StatusUnchanged,
		},
		model.Node{
			ID: "compose:service:db", Name: "db", Type: model.NodeDatabase,
			Source: model.Source{File: "compose.yaml", Line: 1},
			Metadata: map[string]string{
				"image": "postgres:16",
				"ports": "5432:5432",
			},
			Status: model.StatusUnchanged,
		},
	)

	risks := Graph(graph)
	if len(risks) < 2 {
		t.Fatalf("risks = %+v, want public CIDR and published port", risks)
	}
	var public, ports bool
	for _, item := range risks {
		if item.Severity == "high" && strings.Contains(item.Message, "0.0.0.0/0") {
			public = true
		}
		if item.Severity == "high" && strings.Contains(item.Message, "publishes host ports") {
			ports = true
		}
	}
	if !public || !ports {
		t.Fatalf("risks = %+v, want both rules", risks)
	}
}

func TestChangesFindsDeletedDatabase(t *testing.T) {
	graph := model.New()
	graph.Nodes = append(graph.Nodes, model.Node{
		ID: "tf:aws_db_instance.main", Name: "main", Type: model.NodeDatabase,
		Source: model.Source{File: "main.tf", Line: 1},
		Metadata: map[string]string{
			"engine": "postgres",
		},
		Status: model.StatusRemoved,
	})

	risks := Changes(graph)
	if len(risks) != 1 || risks[0].Severity != "high" ||
		!strings.Contains(risks[0].Message, "will be deleted") {
		t.Fatalf("risks = %+v", risks)
	}
}
