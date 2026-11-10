package terraform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

const (
	awsFixture = "../../../testdata/terraform/aws"
	planDir    = "../../../testdata/plans"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"hcl file", filepath.Join(awsFixture, "network.tf"), true},
		{"plan json", filepath.Join(planDir, "aws-plan.json"), true},
		{"plain json", filepath.Join(planDir, "not-a-plan.json"), false},
		{"broken json", filepath.Join(planDir, "broken-plan.json"), false},
		{"compose yaml", "../../../testdata/compose/3-tier/compose.yaml", false},
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

func TestParseHCLAWS(t *testing.T) {
	graph, err := New().Parse(awsFixture)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(graph.Groups) != 1 || graph.Groups[0].ID != "group:module:root" {
		t.Fatalf("groups = %+v, want one root module group", graph.Groups)
	}
	if len(graph.Nodes) != 15 {
		t.Fatalf("node count = %d, want 15", len(graph.Nodes))
	}

	checks := []struct {
		id       string
		nodeType model.NodeType
		metadata map[string]string
	}{
		{"tf:aws_vpc.main", model.NodeNetwork, map[string]string{"cidr_block": "10.0.0.0/16"}},
		{"tf:aws_lb.main", model.NodeLoadBalancer, map[string]string{"load_balancer_type": "application", "internal": "false"}},
		{"tf:aws_db_instance.main", model.NodeDatabase, map[string]string{"engine": "postgres", "engine_version": "16.3", "instance_class": "db.t4g.medium"}},
		{"tf:aws_s3_bucket.assets", model.NodeStorage, map[string]string{"bucket": "shop-assets-prod"}},
		{"tf:aws_security_group.app", model.NodeNetwork, nil},
		{"tf:aws_ecs_service.app", model.NodeCompute, map[string]string{"desired_count": "2", "launch_type": "FARGATE"}},
	}

	for _, want := range checks {
		if want.nodeType == "" {
			if _, ok := graph.Node(want.id); ok {
				t.Fatalf("unexpected node %q", want.id)
			}
			continue
		}
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
		if node.Source.File == "" || node.Source.Line == 0 {
			t.Fatalf("%s has no source location", want.id)
		}
	}

	wantEdges := []struct{ from, to string }{
		{"tf:aws_subnet.public_a", "tf:aws_vpc.main"},
		{"tf:aws_subnet.public_b", "tf:aws_vpc.main"},
		{"tf:aws_security_group.alb", "tf:aws_vpc.main"},
		{"tf:aws_security_group.app", "tf:aws_security_group.alb"},
		{"tf:aws_lb.main", "tf:aws_subnet.public_a"},
		{"tf:aws_lb.main", "tf:aws_subnet.public_b"},
		{"tf:aws_lb.main", "tf:aws_security_group.alb"},
		{"tf:aws_lb_listener.https", "tf:aws_lb.main"},
		{"tf:aws_lb_listener.https", "tf:aws_lb_target_group.app"},
		{"tf:aws_ecs_service.app", "tf:aws_ecs_cluster.main"},
		{"tf:aws_ecs_service.app", "tf:aws_ecs_task_definition.app"},
		{"tf:aws_ecs_service.app", "tf:aws_lb_listener.https"},
		{"tf:aws_db_instance.main", "tf:aws_db_subnet_group.main"},
		{"tf:aws_db_instance.main", "tf:aws_security_group.app"},
		{"tf:aws_s3_bucket_versioning.assets", "tf:aws_s3_bucket.assets"},
	}
	for _, want := range wantEdges {
		if !hasEdge(graph, want.from, want.to) {
			t.Fatalf("missing edge %s -> %s", want.from, want.to)
		}
	}
}

func TestParseSingleTFFileLoadsModule(t *testing.T) {
	graph, err := New().Parse(filepath.Join(awsFixture, "network.tf"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 15 {
		t.Fatalf("node count = %d, want the whole module (15)", len(graph.Nodes))
	}
}

func TestParsePlan(t *testing.T) {
	graph, err := New().Parse(filepath.Join(planDir, "aws-plan.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(graph.Nodes) != 6 {
		t.Fatalf("node count = %d, want 6 (data sources skipped)", len(graph.Nodes))
	}

	statuses := map[string]model.Status{
		"tf:aws_vpc.main":         model.StatusUnchanged,
		"tf:aws_subnet.public_a":  model.StatusUnchanged,
		"tf:aws_s3_bucket.assets": model.StatusAdded,
		"tf:aws_db_instance.main": model.StatusChanged,
		"tf:aws_ecs_service.app":  model.StatusRemoved,
		"tf:aws_instance.web":     model.StatusChanged,
	}
	for id, want := range statuses {
		node, ok := graph.Node(id)
		if !ok {
			t.Fatalf("missing node %q", id)
		}
		if node.Status != want {
			t.Fatalf("%s status = %q, want %q", id, node.Status, want)
		}
		if node.Source.Line != 0 {
			t.Fatalf("%s source line = %d, want 0 for plan JSON", id, node.Source.Line)
		}
	}

	instance, _ := graph.Node("tf:aws_instance.web")
	if instance.Metadata["actions"] != "delete,create" {
		t.Fatalf("actions metadata = %q", instance.Metadata["actions"])
	}
	if graph.Nodes[0].Group != "group:module:root" {
		t.Fatalf("group = %q, want root module", graph.Nodes[0].Group)
	}
	if !hasEdge(graph, "tf:aws_subnet.public_a", "tf:aws_vpc.main") {
		t.Fatal("missing reference edge from subnet to VPC")
	}
}

func TestParseErrors(t *testing.T) {
	t.Run("broken plan", func(t *testing.T) {
		_, err := New().Parse(filepath.Join(planDir, "broken-plan.json"))
		if err == nil || !strings.Contains(err.Error(), "invalid plan JSON") {
			t.Fatalf("Parse() = %v, want invalid plan JSON", err)
		}
	})
	t.Run("not a plan", func(t *testing.T) {
		_, err := New().Parse(filepath.Join(planDir, "not-a-plan.json"))
		if err == nil || !strings.Contains(err.Error(), "format_version") {
			t.Fatalf("Parse() = %v, want a format_version error", err)
		}
	})
	t.Run("no resources", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "variables.tf"),
			[]byte("variable \"region\" { default = \"us-east-1\" }\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := New().Parse(dir)
		if err == nil || !strings.Contains(err.Error(), "no resource or data blocks") {
			t.Fatalf("Parse() = %v, want no resource or data blocks", err)
		}
	})
	t.Run("syntax error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.tf"),
			[]byte("resource \"aws_vpc\" \"main\" {\n  cidr_block =\n}\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := New().Parse(dir)
		if err == nil || !strings.Contains(err.Error(), "terraform:") {
			t.Fatalf("Parse() = %v, want a Terraform syntax error", err)
		}
	})
	t.Run("duplicate resource", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n")
		if err := os.WriteFile(filepath.Join(dir, "a.tf"), content, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.tf"), content, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := New().Parse(dir)
		if err == nil || !strings.Contains(err.Error(), "declared twice") {
			t.Fatalf("Parse() = %v, want declared twice", err)
		}
	})
}

func TestStatusForActions(t *testing.T) {
	tests := []struct {
		actions []string
		want    model.Status
		wantErr bool
	}{
		{[]string{"no-op"}, model.StatusUnchanged, false},
		{[]string{"read"}, model.StatusUnchanged, false},
		{[]string{"create"}, model.StatusAdded, false},
		{[]string{"update"}, model.StatusChanged, false},
		{[]string{"delete"}, model.StatusRemoved, false},
		{[]string{"delete", "create"}, model.StatusChanged, false},
		{[]string{"forget"}, "", true},
		{nil, "", true},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.actions, "+"), func(t *testing.T) {
			got, err := statusForActions(tt.actions)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("statusForActions(%v) = %q, want error", tt.actions, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("statusForActions(%v) = (%q, %v), want %q", tt.actions, got, err, tt.want)
			}
		})
	}
}

func TestGroupForAddress(t *testing.T) {
	tests := []struct {
		address  string
		typ      string
		name     string
		wantID   string
		wantName string
	}{
		{"aws_vpc.main", "aws_vpc", "main", "group:module:root", "root"},
		{"module.app.aws_instance.web", "aws_instance", "web", "group:module:app", "app"},
		{"module.app.module.db.aws_db_instance.main", "aws_db_instance", "main", "group:module:app.db", "app.db"},
		{"aws_instance.web[0]", "aws_instance", "web", "group:module:root", "root"},
	}
	for _, tt := range tests {
		id, name := groupForAddress(tt.address, tt.typ, tt.name)
		if id != tt.wantID || name != tt.wantName {
			t.Fatalf("groupForAddress(%q) = (%q, %q), want (%q, %q)",
				tt.address, id, name, tt.wantID, tt.wantName)
		}
	}
}

func TestLocalAddress(t *testing.T) {
	tests := []struct {
		reference string
		want      string
		ok        bool
	}{
		{"aws_vpc.main.id", "aws_vpc.main", true},
		{"aws_instance.web[0].id", "aws_instance.web", true},
		{"aws_security_group.alb", "aws_security_group.alb", true},
		{"var.region", "", false},
		{"local.name", "", false},
		{"module.app.aws_instance.web.id", "", false},
		{"data.aws_ami.ubuntu.id", "", false},
	}
	for _, tt := range tests {
		got, ok := localAddress(tt.reference)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("localAddress(%q) = (%q, %v), want (%q, %v)",
				tt.reference, got, ok, tt.want, tt.ok)
		}
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

func TestParseModuleTree(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("main.tf", `
module "network" {
  source = "./modules/network"
}

module "app" {
  source = "./modules/app"
  count  = 2
}

resource "aws_instance" "web" {
  ami           = "ami-123"
  instance_type = "t3.micro"
  subnet_id     = module.network.subnet_id
  tags = {
    Name = "web"
    Team = "platform"
  }
}

resource "aws_instance" "workers" {
  count = 2
  ami   = data.aws_ami.ubuntu.id
  for_each = {
    a = 1
    b = 2
  }
}

data "aws_ami" "ubuntu" {
  most_recent = true
}
`)
	write("modules/network/main.tf", `
resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "public" {
  vpc_id     = aws_vpc.main.id
  cidr_block = "10.0.1.0/24"
}

resource "aws_security_group" "web" {
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

output "subnet_id" {
  value = aws_subnet.public.id
}
`)
	write("modules/app/main.tf", `
resource "aws_instance" "web" {
  instance_type = "t3.small"
}
`)

	graph, err := New().Parse(dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Groups) != 3 {
		t.Fatalf("groups = %+v, want root, network, and app groups", graph.Groups)
	}
	var appGroup model.Group
	for _, group := range graph.Groups {
		if group.ID == "group:module:app" {
			appGroup = group
		}
	}
	if appGroup.Metadata["count"] != "2" {
		t.Fatalf("app group metadata = %+v, want module count", appGroup.Metadata)
	}
	for _, id := range []string{
		"tf:aws_instance.web",
		"tf:aws_instance.workers",
		"tf:data.aws_ami.ubuntu",
		"tf:module.network.aws_vpc.main",
		"tf:module.network.aws_subnet.public",
		"tf:module.app.aws_instance.web",
	} {
		if _, ok := graph.Node(id); !ok {
			t.Fatalf("missing %s", id)
		}
	}

	web, _ := graph.Node("tf:aws_instance.web")
	if web.Metadata["instance_type"] != "t3.micro" || web.Metadata["tags_hash"] == "" {
		t.Fatalf("web metadata = %+v", web.Metadata)
	}
	group, _ := graph.Node("tf:module.network.aws_security_group.web")
	if group.Metadata["ingress_cidrs"] != "0.0.0.0/0" {
		t.Fatalf("security group metadata = %+v", group.Metadata)
	}
	workers, _ := graph.Node("tf:aws_instance.workers")
	if workers.Metadata["count"] != "2" || workers.Metadata["for_each"] != "a, b" {
		t.Fatalf("workers metadata = %+v", workers.Metadata)
	}
	if !hasEdge(graph, "tf:aws_instance.web", "tf:module.network.aws_subnet.public") {
		t.Fatal("module output edge is missing")
	}
	if !hasEdge(graph, "tf:aws_instance.workers", "tf:data.aws_ami.ubuntu") {
		t.Fatal("data source edge is missing")
	}
	if !hasEdge(graph, "tf:module.network.aws_subnet.public", "tf:module.network.aws_vpc.main") {
		t.Fatal("module resource edge is missing")
	}
}

func TestParseModuleOnlyRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "child"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"),
		[]byte("module \"child\" {\n  source = \"./child\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child", "main.tf"),
		[]byte("resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write child: %v", err)
	}

	graph, err := New().Parse(dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := graph.Node("tf:module.child.aws_vpc.main"); !ok {
		t.Fatalf("child node is missing: %+v", graph.Nodes)
	}
}
