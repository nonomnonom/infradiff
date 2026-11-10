package terraform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/infradiff/infradiff/internal/model"
	"github.com/infradiff/infradiff/internal/parser/infer"
	"github.com/infradiff/infradiff/internal/redact"
	"github.com/zclconf/go-cty/cty"
)

const rootGroupID = "group:module:root"

// literalMetadataAttributes are copied into node metadata when their value is
// a literal. Everything else is ignored to avoid leaking configuration values.
var literalMetadataAttributes = map[string]bool{
	"ami":                 true,
	"allocated_storage":   true,
	"availability_zone":   true,
	"bucket":              true,
	"cidr_block":          true,
	"cpu":                 true,
	"desired_count":       true,
	"engine":              true,
	"engine_version":      true,
	"family":              true,
	"handler":             true,
	"instance_class":      true,
	"internal":            true,
	"launch_type":         true,
	"load_balancer_type":  true,
	"memory":              true,
	"memory_size":         true,
	"multi_az":            true,
	"port":                true,
	"protocol":            true,
	"publicly_accessible": true,
	"runtime":             true,
}

type resourceBlock struct {
	typeName string
	name     string
	file     string
	line     int
	body     *hclsyntax.Body
}

type parsedFile struct {
	path string
	body *hclsyntax.Body
}

func parseHCLDir(dir string) (*model.Graph, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("terraform: %s: %w", dir, err)
	}

	parser := hclparse.NewParser()
	files := make([]parsedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			return nil, fmt.Errorf("terraform: %s", strings.TrimSpace(diags.Error()))
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			return nil, fmt.Errorf("terraform: %s: unsupported HCL syntax", path)
		}
		files = append(files, parsedFile{path: path, body: body})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	if len(files) == 0 {
		return nil, fmt.Errorf("terraform: %s: no .tf files found; add Terraform configuration", dir)
	}

	blocks, err := collectResources(files)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("terraform: %s: no resource blocks found; add at least one resource", dir)
	}

	graph := model.New()
	graph.Groups = append(graph.Groups, model.Group{
		ID:     rootGroupID,
		Name:   "root",
		Kind:   model.GroupModule,
		Status: model.StatusUnchanged,
	})

	types := make(map[string]map[string]bool, len(blocks))
	for _, block := range blocks {
		if types[block.typeName] == nil {
			types[block.typeName] = make(map[string]bool)
		}
		types[block.typeName][block.name] = true
	}

	for _, block := range blocks {
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       nodeID(block.typeName, block.name),
			Name:     block.name,
			Type:     infer.ResourceType(block.typeName),
			Group:    rootGroupID,
			Source:   model.Source{File: block.file, Line: block.line},
			Metadata: redact.Metadata(blockMetadata(block)),
			Status:   model.StatusUnchanged,
		})
	}

	seenEdges := make(map[string]bool)
	for _, block := range blocks {
		collector := &traversalCollector{}
		if diags := hclsyntax.Walk(block.body, collector); diags.HasErrors() {
			return nil, fmt.Errorf("terraform: %s:%d: %s",
				block.file, block.line, strings.TrimSpace(diags.Error()))
		}
		for _, traversal := range collector.traversals {
			targetType, targetName, ok := resourceReference(traversal, types)
			if !ok {
				continue
			}
			if targetType == block.typeName && targetName == block.name {
				continue
			}
			from := nodeID(block.typeName, block.name)
			to := nodeID(targetType, targetName)
			key := from + "->" + to
			if seenEdges[key] {
				continue
			}
			seenEdges[key] = true
			graph.Edges = append(graph.Edges, model.Edge{
				From:   from,
				To:     to,
				Kind:   model.EdgeReference,
				Status: model.StatusUnchanged,
			})
		}
	}

	graph.Sort()
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func collectResources(files []parsedFile) ([]resourceBlock, error) {
	var blocks []resourceBlock
	seen := make(map[string]resourceBlock)
	for _, file := range files {
		for _, block := range file.body.Blocks {
			if block.Type != "resource" {
				continue
			}
			if len(block.Labels) != 2 {
				return nil, fmt.Errorf("terraform: %s:%d: resource block needs a type and a name",
					file.path, block.TypeRange.Start.Line)
			}
			resource := resourceBlock{
				typeName: block.Labels[0],
				name:     block.Labels[1],
				file:     file.path,
				line:     block.TypeRange.Start.Line,
				body:     block.Body,
			}
			id := resource.typeName + "." + resource.name
			if previous, exists := seen[id]; exists {
				return nil, fmt.Errorf(
					"terraform: resource %q %q is declared twice (%s and %s); remove the duplicate",
					resource.typeName, resource.name, previous.file, resource.file)
			}
			seen[id] = resource
			blocks = append(blocks, resource)
		}
	}
	return blocks, nil
}

func blockMetadata(block resourceBlock) map[string]string {
	metadata := map[string]string{"resource_type": block.typeName}
	for name, attribute := range block.body.Attributes {
		if !literalMetadataAttributes[name] {
			continue
		}
		if value, ok := literalValue(attribute.Expr); ok {
			metadata[name] = value
		}
	}
	return metadata
}

func literalValue(expression hclsyntax.Expression) (string, bool) {
	value, diags := expression.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() {
		return "", false
	}
	switch {
	case value.Type().Equals(cty.String):
		return value.AsString(), true
	case value.Type().Equals(cty.Number):
		return value.AsBigFloat().Text('f', -1), true
	case value.Type().Equals(cty.Bool):
		if value.True() {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

type traversalCollector struct {
	traversals []hcl.Traversal
}

func (c *traversalCollector) Enter(node hclsyntax.Node) hcl.Diagnostics {
	if expression, ok := node.(*hclsyntax.ScopeTraversalExpr); ok {
		c.traversals = append(c.traversals, expression.Traversal)
	}
	return nil
}

func (c *traversalCollector) Exit(hclsyntax.Node) hcl.Diagnostics {
	return nil
}

func resourceReference(traversal hcl.Traversal, types map[string]map[string]bool) (string, string, bool) {
	if len(traversal) < 2 {
		return "", "", false
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok {
		return "", "", false
	}
	attribute, ok := traversal[1].(hcl.TraverseAttr)
	if !ok {
		return "", "", false
	}
	if !types[root.Name][attribute.Name] {
		return "", "", false
	}
	return root.Name, attribute.Name, true
}

func nodeID(resourceType, name string) string {
	return "tf:" + resourceType + "." + name
}
