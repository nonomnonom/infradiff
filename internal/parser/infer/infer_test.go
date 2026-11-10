package infer

import (
	"testing"

	"github.com/infradiff/infradiff/internal/model"
)

func TestResourceType(t *testing.T) {
	tests := []struct {
		resource string
		want     model.NodeType
	}{
		{"aws_vpc", model.NodeNetwork},
		{"aws_subnet", model.NodeNetwork},
		{"aws_security_group", model.NodeNetwork},
		{"aws_lb", model.NodeLoadBalancer},
		{"aws_lb_target_group", model.NodeNetwork},
		{"aws_db_instance", model.NodeDatabase},
		{"aws_dynamodb_table", model.NodeDatabase},
		{"aws_elasticache_cluster", model.NodeCache},
		{"aws_sqs_queue", model.NodeQueue},
		{"aws_s3_bucket", model.NodeStorage},
		{"aws_instance", model.NodeCompute},
		{"aws_ecs_service", model.NodeCompute},
		{"aws_lambda_function", model.NodeFunction},
		{"google_sql_database_instance", model.NodeDatabase},
		{"azurerm_linux_virtual_machine", model.NodeOther},
	}

	for _, tt := range tests {
		t.Run(tt.resource, func(t *testing.T) {
			if got := ResourceType(tt.resource); got != tt.want {
				t.Fatalf("ResourceType(%q) = %q, want %q", tt.resource, got, tt.want)
			}
		})
	}
}

func TestImageType(t *testing.T) {
	tests := []struct {
		image string
		want  model.NodeType
	}{
		{"postgres:16", model.NodeDatabase},
		{"bitnami/postgresql:16", model.NodeDatabase},
		{"mysql:8.4", model.NodeDatabase},
		{"mongo:7", model.NodeDatabase},
		{"redis:7", model.NodeCache},
		{"valkey/valkey:8", model.NodeCache},
		{"rabbitmq:3-management", model.NodeQueue},
		{"bitnami/kafka:3.7", model.NodeQueue},
		{"minio/minio:latest", model.NodeStorage},
		{"nginx:1.29", model.NodeLoadBalancer},
		{"traefik:v3", model.NodeLoadBalancer},
		{"shop/api:latest", model.NodeService},
		{"", model.NodeOther},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := ImageType(tt.image); got != tt.want {
				t.Fatalf("ImageType(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}
