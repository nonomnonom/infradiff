// Package infer maps technology names to graph node types.
package infer

import (
	"strings"

	"github.com/infradiff/infradiff/internal/model"
)

// This file is the single place where technology names map to node types.
// Rules are checked in order, so put the most specific matches first.

var imageTypeRules = []struct {
	nodeType model.NodeType
	needles  []string
}{
	{model.NodeDatabase, []string{
		"postgres", "postgresql", "mysql", "mariadb", "mongo", "cockroach",
		"mssql", "oracle", "clickhouse", "cassandra", "neo4j",
	}},
	{model.NodeCache, []string{"redis", "valkey", "memcached", "dragonfly"}},
	{model.NodeQueue, []string{"rabbitmq", "kafka", "nats", "activemq", "pulsar", "zookeeper"}},
	{model.NodeStorage, []string{"minio", "ceph", "seaweedfs", "localstack-s3"}},
	{model.NodeLoadBalancer, []string{
		"nginx", "traefik", "haproxy", "envoy", "caddy", "httpd", "kong", "apisix",
	}},
}

// terraformExactTypes covers the resource types used by the supported examples
// and common AWS setups. Prefix rules below handle the long tail.
var terraformExactTypes = map[string]model.NodeType{
	"aws_db_instance":                   model.NodeDatabase,
	"aws_rds_cluster":                   model.NodeDatabase,
	"aws_dynamodb_table":                model.NodeDatabase,
	"aws_elasticache_cluster":           model.NodeCache,
	"aws_elasticache_replication_group": model.NodeCache,
	"aws_sqs_queue":                     model.NodeQueue,
	"aws_sns_topic":                     model.NodeQueue,
	"aws_msk_cluster":                   model.NodeQueue,
	"aws_mq_broker":                     model.NodeQueue,
	"aws_kinesis_stream":                model.NodeQueue,
	"aws_s3_bucket":                     model.NodeStorage,
	"aws_efs_file_system":               model.NodeStorage,
	"aws_ebs_volume":                    model.NodeStorage,
	"aws_lb":                            model.NodeLoadBalancer,
	"aws_alb":                           model.NodeLoadBalancer,
	"aws_elb":                           model.NodeLoadBalancer,
	"aws_cloudfront_distribution":       model.NodeLoadBalancer,
	"aws_api_gateway_rest_api":          model.NodeIngress,
	"aws_apigatewayv2_api":              model.NodeIngress,
	"aws_lb_target_group":               model.NodeNetwork,
	"aws_lb_listener":                   model.NodeNetwork,
	"aws_vpc":                           model.NodeNetwork,
	"aws_subnet":                        model.NodeNetwork,
	"aws_security_group":                model.NodeNetwork,
	"aws_db_subnet_group":               model.NodeNetwork,
	"aws_route_table":                   model.NodeNetwork,
	"aws_internet_gateway":              model.NodeNetwork,
	"aws_nat_gateway":                   model.NodeNetwork,
	"aws_instance":                      model.NodeCompute,
	"aws_autoscaling_group":             model.NodeCompute,
	"aws_ecs_service":                   model.NodeCompute,
	"aws_ecs_cluster":                   model.NodeCompute,
	"aws_eks_cluster":                   model.NodeCompute,
	"aws_launch_template":               model.NodeCompute,
	"aws_lambda_function":               model.NodeFunction,
}

var terraformPrefixRules = []struct {
	needle   string
	nodeType model.NodeType
}{{"db_instance", model.NodeDatabase},
	{"sql_database", model.NodeDatabase},
	{"rds_cluster", model.NodeDatabase},
	{"dynamodb_table", model.NodeDatabase},
	{"elasticache", model.NodeCache},
	{"sqs_queue", model.NodeQueue},
	{"sns_topic", model.NodeQueue},
	{"s3_bucket", model.NodeStorage},
	{"load_balancer", model.NodeLoadBalancer},
	{"lambda_function", model.NodeFunction},
	{"security_group", model.NodeNetwork},
	{"subnet", model.NodeNetwork},
	{"vpc", model.NodeNetwork},
	{"ecs_service", model.NodeCompute},
	{"autoscaling_group", model.NodeCompute},
}

// ResourceType guesses a node type from a Terraform resource type, for example
// aws_db_instance or google_sql_database_instance.
func ResourceType(resourceType string) model.NodeType {
	name := strings.ToLower(resourceType)
	if nodeType, ok := terraformExactTypes[name]; ok {
		return nodeType
	}
	for _, rule := range terraformPrefixRules {
		if strings.Contains(name, rule.needle) {
			return rule.nodeType
		}
	}
	return model.NodeOther
}

// ImageType guesses a node type from a container image reference.
func ImageType(image string) model.NodeType {
	name := strings.ToLower(image)
	for _, rule := range imageTypeRules {
		for _, needle := range rule.needles {
			if strings.Contains(name, needle) {
				return rule.nodeType
			}
		}
	}
	if name == "" {
		return model.NodeOther
	}
	return model.NodeService
}
