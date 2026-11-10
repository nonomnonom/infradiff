package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/parser/infer"
	"github.com/nonomnonom/infradiff/internal/redact"
)

type planFile struct {
	FormatVersion    string             `json:"format_version"`
	TerraformVersion string             `json:"terraform_version"`
	ResourceChanges  []resourceChange   `json:"resource_changes"`
	Configuration    *planConfiguration `json:"configuration"`
}

type resourceChange struct {
	Address      string `json:"address"`
	Mode         string `json:"mode"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	ProviderName string `json:"provider_name"`
	Change       change `json:"change"`
}

type change struct {
	Actions []string `json:"actions"`
}

type planConfiguration struct {
	RootModule *planModule `json:"root_module"`
}

type planModule struct {
	Address      string           `json:"address"`
	Resources    []configResource `json:"resources"`
	ChildModules []planModule     `json:"child_modules"`
}

type configResource struct {
	Address     string                `json:"address"`
	Mode        string                `json:"mode"`
	Type        string                `json:"type"`
	Name        string                `json:"name"`
	Expressions map[string]configExpr `json:"expressions"`
}

type configExpr struct {
	References []string `json:"references"`
}

func parsePlan(path string) (*model.Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("terraform: %s: %w", path, err)
	}
	var plan planFile
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("terraform: %s: invalid plan JSON: %w", path, err)
	}
	if plan.FormatVersion == "" {
		return nil, fmt.Errorf("terraform: %s: not a Terraform plan (missing format_version); use terraform show -json", path)
	}
	if len(plan.ResourceChanges) == 0 {
		return nil, fmt.Errorf("terraform: %s: plan has no resource_changes; nothing to diagram", path)
	}

	graph := model.New()
	groups := make(map[string]bool)
	type planNode struct {
		id       string
		typeName string
		name     string
	}
	var nodes []planNode
	localIndex := make(map[string][]string)
	seenAddresses := make(map[string]bool)

	for _, resource := range plan.ResourceChanges {
		if resource.Mode != "managed" {
			continue
		}
		if resource.Address == "" {
			return nil, fmt.Errorf("terraform: %s: resource change without an address", path)
		}
		if seenAddresses[resource.Address] {
			return nil, fmt.Errorf("terraform: %s: duplicate resource change %q", path, resource.Address)
		}
		seenAddresses[resource.Address] = true

		status, err := statusForActions(resource.Change.Actions)
		if err != nil {
			return nil, fmt.Errorf("terraform: %s: resource %q: %w", path, resource.Address, err)
		}

		id := "tf:" + resource.Address
		name := resource.Name
		if name == "" {
			name = resource.Address
		}
		groupID, groupName := groupForAddress(resource.Address, resource.Type, resource.Name)
		if !groups[groupID] {
			groups[groupID] = true
			graph.Groups = append(graph.Groups, model.Group{
				ID:     groupID,
				Name:   groupName,
				Kind:   model.GroupModule,
				Status: model.StatusUnchanged,
			})
		}

		metadata := map[string]string{"resource_type": resource.Type}
		if provider := shortProvider(resource.ProviderName); provider != "" {
			metadata["provider"] = provider
		}
		if len(resource.Change.Actions) > 0 {
			metadata["actions"] = strings.Join(resource.Change.Actions, ",")
		}

		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       id,
			Name:     name,
			Type:     infer.ResourceType(resource.Type),
			Group:    groupID,
			Source:   model.Source{File: path, Line: 0},
			Metadata: redact.Metadata(metadata),
			Status:   status,
		})
		nodes = append(nodes, planNode{id: id, typeName: resource.Type, name: resource.Name})

		local := resource.Type + "." + resource.Name
		localIndex[local] = append(localIndex[local], id)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("terraform: %s: plan has no managed resource changes", path)
	}

	seenEdges := make(map[string]bool)
	if plan.Configuration != nil {
		var configResources []configResource
		collectConfigResources(plan.Configuration.RootModule, &configResources)
		for _, resource := range configResources {
			if resource.Mode != "" && resource.Mode != "managed" {
				continue
			}
			from := "tf:" + resource.Address
			if !seenAddresses[resource.Address] {
				continue
			}
			for _, expression := range resource.Expressions {
				for _, reference := range expression.References {
					local, ok := localAddress(reference)
					if !ok {
						continue
					}
					targets := localIndex[local]
					if len(targets) != 1 || targets[0] == from {
						continue
					}
					key := from + "->" + targets[0]
					if seenEdges[key] {
						continue
					}
					seenEdges[key] = true
					graph.Edges = append(graph.Edges, model.Edge{
						From:   from,
						To:     targets[0],
						Kind:   model.EdgeReference,
						Status: model.StatusUnchanged,
					})
				}
			}
		}
	}

	graph.Sort()
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func statusForActions(actions []string) (model.Status, error) {
	set := make(map[string]bool, len(actions))
	for _, action := range actions {
		set[action] = true
	}
	switch {
	case set["create"] && set["delete"]:
		return model.StatusChanged, nil
	case set["create"]:
		return model.StatusAdded, nil
	case set["delete"]:
		return model.StatusRemoved, nil
	case set["update"]:
		return model.StatusChanged, nil
	case set["no-op"], set["read"]:
		return model.StatusUnchanged, nil
	default:
		return "", fmt.Errorf("unsupported change actions %v", actions)
	}
}

func groupForAddress(address, resourceType, name string) (string, string) {
	suffix := resourceType + "." + name
	index := strings.LastIndex(address, suffix)
	if index <= 0 {
		return rootGroupID, "root"
	}
	prefix := strings.TrimSuffix(address[:index], ".")
	if prefix == "" {
		return rootGroupID, "root"
	}
	// Terraform addresses say module.network.module.database, while HCL
	// module paths and groups use network.database.
	parts := strings.Split(prefix, ".")
	modules := parts[:0]
	for _, part := range parts {
		if part != "module" {
			modules = append(modules, part)
		}
	}
	prefix = strings.Join(modules, ".")
	if prefix == "" {
		return rootGroupID, "root"
	}
	return "group:module:" + prefix, prefix
}

func shortProvider(providerName string) string {
	if providerName == "" {
		return ""
	}
	parts := strings.Split(providerName, "/")
	return parts[len(parts)-1]
}

func collectConfigResources(module *planModule, out *[]configResource) {
	if module == nil {
		return
	}
	*out = append(*out, module.Resources...)
	for i := range module.ChildModules {
		child := module.ChildModules[i]
		collectConfigResources(&child, out)
	}
}

// localAddress reduces a plan reference such as
// "aws_subnet.public_a.id" or "aws_instance.web[0].id" to "aws_subnet.public_a".
// Module-qualified references are skipped because their local names can be
// ambiguous across modules.
func localAddress(reference string) (string, bool) {
	parts := strings.Split(reference, ".")
	if len(parts) < 2 {
		return "", false
	}
	resourceType := stripIndex(parts[0])
	name := stripIndex(parts[1])
	switch resourceType {
	case "var", "local", "module", "data", "each", "count", "path", "terraform", "self":
		return "", false
	}
	if resourceType == "" || name == "" {
		return "", false
	}
	return resourceType + "." + name, true
}

func stripIndex(value string) string {
	if index := strings.IndexByte(value, '['); index >= 0 {
		return value[:index]
	}
	return value
}
