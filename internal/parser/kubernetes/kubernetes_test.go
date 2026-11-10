package kubernetes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

const (
	shopDir = "../../../testdata/kubernetes/shop"
	fixDir  = "../../../testdata/kubernetes"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"deployment manifest", shopDir + "/api.yaml", true},
		{"config map manifest", shopDir + "/config.yaml", true},
		{"compose file", "../../../testdata/compose/3-tier/compose.yaml", false},
		{"terraform plan", "../../../testdata/plans/aws-plan.json", false},
		{"plain yaml", "../../../testdata/compose/not-compose.yaml", true},
	}

	parser := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.Detect(tt.path); got != tt.want {
				t.Fatalf("Detect(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseShopDirectory(t *testing.T) {
	graph, err := New().ParseDir(shopDir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}

	if len(graph.Groups) != 1 || graph.Groups[0].ID != "group:namespace:shop" {
		t.Fatalf("groups = %+v, want one shop namespace", graph.Groups)
	}
	if len(graph.Nodes) != 12 {
		t.Fatalf("node count = %d, want 12", len(graph.Nodes))
	}
	if len(graph.Edges) != 10 {
		t.Fatalf("edge count = %d, want 10", len(graph.Edges))
	}

	checks := []struct {
		id       string
		nodeType model.NodeType
		metadata map[string]string
	}{
		{"k8s:deployment:shop/api", model.NodeCompute, map[string]string{"images": "shop/api:2", "replicas": "2"}},
		{"k8s:deployment:shop/web", model.NodeCompute, map[string]string{"images": "nginx:1.29", "replicas": "1"}},
		{"k8s:statefulset:shop/db", model.NodeCompute, map[string]string{"images": "postgres:16"}},
		{"k8s:daemonset:shop/log-agent", model.NodeCompute, map[string]string{"images": "fluent/fluent-bit:3.1"}},
		{"k8s:service:shop/web", model.NodeLoadBalancer, map[string]string{"type": "LoadBalancer", "ports": "http:80/TCP", "selector": "app=web"}},
		{"k8s:service:shop/api", model.NodeService, map[string]string{"type": "ClusterIP", "ports": "http:8080/TCP"}},
		{"k8s:ingress:shop/web", model.NodeIngress, map[string]string{"hosts": "shop.example.com"}},
		{"k8s:persistentvolumeclaim:shop/data", model.NodeStorage, map[string]string{"storage_class": "gp3", "storage": "20Gi", "access_modes": "ReadWriteOnce"}},
		{"k8s:secret:shop/db-credentials", model.NodeOther, map[string]string{"type": "Opaque"}},
	}

	for _, want := range checks {
		node, ok := graph.Node(want.id)
		if !ok {
			t.Fatalf("missing node %q", want.id)
		}
		if node.Type != want.nodeType {
			t.Fatalf("%s type = %q, want %q", want.id, node.Type, want.nodeType)
		}
		for key, value := range want.metadata {
			if node.Metadata[key] != value {
				t.Fatalf("%s metadata[%q] = %q, want %q", want.id, key, node.Metadata[key], value)
			}
		}
		if node.Group != "group:namespace:shop" {
			t.Fatalf("%s group = %q", want.id, node.Group)
		}
	}

	wantEdges := []struct{ from, to string }{
		{"k8s:service:shop/api", "k8s:deployment:shop/api"},
		{"k8s:service:shop/web", "k8s:deployment:shop/web"},
		{"k8s:service:shop/db", "k8s:statefulset:shop/db"},
		{"k8s:ingress:shop/web", "k8s:service:shop/web"},
		{"k8s:deployment:shop/api", "k8s:configmap:shop/app-config"},
		{"k8s:deployment:shop/api", "k8s:secret:shop/db-credentials"},
		{"k8s:deployment:shop/api", "k8s:persistentvolumeclaim:shop/uploads"},
		{"k8s:statefulset:shop/db", "k8s:secret:shop/db-credentials"},
		{"k8s:statefulset:shop/db", "k8s:persistentvolumeclaim:shop/data"},
		{"k8s:daemonset:shop/log-agent", "k8s:configmap:shop/app-config"},
	}
	for _, want := range wantEdges {
		if !hasEdge(graph, want.from, want.to) {
			t.Fatalf("missing edge %s -> %s", want.from, want.to)
		}
	}

	for _, node := range graph.Nodes {
		for key, value := range node.Metadata {
			if strings.Contains(value, "c3VwZXJzZWNyZXQ=") {
				t.Fatalf("secret value leaked into %s metadata %q", node.ID, key)
			}
		}
	}
}

func TestParseSingleFile(t *testing.T) {
	graph, err := New().Parse(shopDir + "/api.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2 (Deployment and Service)", len(graph.Nodes))
	}
	if !hasEdge(graph, "k8s:service:shop/api", "k8s:deployment:shop/api") {
		t.Fatal("service selector edge is missing")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"invalid yaml", fixDir + "/invalid-yaml.yaml", "kubernetes:"},
		{"missing name", fixDir + "/missing-name.yaml", "has no metadata.name"},
		{"duplicate object", fixDir + "/duplicate.yaml", "is already defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(tt.path)
			if err == nil {
				t.Fatalf("Parse() = nil, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestParseUnknownKindKept(t *testing.T) {
	graph, err := New().Parse(fixDir + "/unsupported-kind.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	node, ok := graph.Node("k8s:widget:not-supported")
	if !ok {
		t.Fatalf("unknown kind was dropped: %+v", graph.Nodes)
	}
	if node.Type != model.NodeOther || node.Metadata["kind"] != "Widget" {
		t.Fatalf("unknown node = %+v", node)
	}
}

func TestParseDirNoManifests(t *testing.T) {
	_, err := New().ParseDir(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no supported manifests") {
		t.Fatalf("ParseDir() = %v, want no supported manifests", err)
	}
}

func TestLabelsMatch(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		labels   map[string]string
		want     bool
	}{
		{"exact match", map[string]string{"app": "api"}, map[string]string{"app": "api"}, true},
		{"extra labels", map[string]string{"app": "api"}, map[string]string{"app": "api", "tier": "web"}, true},
		{"wrong value", map[string]string{"app": "api"}, map[string]string{"app": "web"}, false},
		{"missing label", map[string]string{"app": "api"}, map[string]string{"tier": "web"}, false},
		{"empty selector", nil, map[string]string{"app": "api"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelsMatch(tt.selector, tt.labels); got != tt.want {
				t.Fatalf("labelsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIngressBackends(t *testing.T) {
	doc := ingressDocument{}
	doc.Spec.DefaultBackend = &ingressBackend{ServiceName: "legacy"}
	doc.Spec.Rules = append(doc.Spec.Rules, struct {
		Host string `yaml:"host"`
		HTTP struct {
			Paths []struct {
				Backend ingressBackend `yaml:"backend"`
			} `yaml:"paths"`
		} `yaml:"http"`
	}{Host: "example.com"})

	services := ingressServices(doc)
	if len(services) != 1 || services[0] != "legacy" {
		t.Fatalf("ingressServices() = %v, want [legacy]", services)
	}
}

func hasEdge(graph *model.Graph, from, to string) bool {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

func TestParseListAndAdditionalKinds(t *testing.T) {
	dir := t.TempDir()
	body := `
apiVersion: v1
kind: List
items:
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: api
      namespace: shop
      labels:
        app: api
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: api
      template:
        metadata:
          labels:
            app: api
        spec:
          containers:
            - name: api
              image: shop/api:2
  - apiVersion: autoscaling/v2
    kind: HorizontalPodAutoscaler
    metadata:
      name: api
      namespace: shop
    spec:
      minReplicas: 1
      maxReplicas: 5
      scaleTargetRef:
        apiVersion: apps/v1
        kind: Deployment
        name: api
  - apiVersion: batch/v1
    kind: CronJob
    metadata:
      name: cleanup
      namespace: shop
    spec:
      schedule: "0 * * * *"
      jobTemplate:
        spec:
          template:
            metadata:
              labels:
                app: cleanup
            spec:
              containers:
                - name: cleanup
                  image: shop/cleanup:1
  - apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: runner
      namespace: shop
  - apiVersion: policy/v1
    kind: PodDisruptionBudget
    metadata:
      name: api
      namespace: shop
  - apiVersion: networking.k8s.io/v1
    kind: NetworkPolicy
    metadata:
      name: api
      namespace: shop
  - apiVersion: apiextensions.k8s.io/v1
    kind: CustomResourceDefinition
    metadata:
      name: widgets.example.com
  - apiVersion: v1
    kind: Namespace
    metadata:
      name: shop
`
	if err := os.WriteFile(filepath.Join(dir, "all.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	graph, err := New().ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(graph.Nodes) != 8 {
		t.Fatalf("node count = %d, want 8: %+v", len(graph.Nodes), graph.Nodes)
	}
	if !hasEdge(graph, "k8s:horizontalpodautoscaler:shop/api", "k8s:deployment:shop/api") {
		t.Fatal("HPA target edge is missing")
	}
	cron, ok := graph.Node("k8s:cronjob:shop/cleanup")
	if !ok || cron.Metadata["schedule"] != "0 * * * *" || cron.Metadata["images"] != "shop/cleanup:1" {
		t.Fatalf("cronjob = %+v", cron)
	}
	crd, ok := graph.Node("k8s:customresourcedefinition:widgets.example.com")
	if !ok || crd.Group != "" {
		t.Fatalf("CRD = %+v, want cluster-scoped with no phantom namespace", crd)
	}
	if _, ok := graph.Node("k8s:namespace:shop"); !ok {
		t.Fatal("Namespace node is missing")
	}
	if len(graph.Groups) != 1 || graph.Groups[0].ID != "group:namespace:shop" {
		t.Fatalf("groups = %+v, want only the real shop namespace", graph.Groups)
	}
}
