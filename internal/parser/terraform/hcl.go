package terraform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/parser/infer"
	"github.com/nonomnonom/infradiff/internal/redact"
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
	"instance_type":       true,
	"internal":            true,
	"launch_type":         true,
	"load_balancer_type":  true,
	"machine_type":        true,
	"memory":              true,
	"memory_size":         true,
	"multi_az":            true,
	"node_type":           true,
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

type moduleScope struct {
	path        string
	dir         string
	groupID     string
	types       map[string]map[string]bool
	dataTypes   map[string]map[string]bool
	outputs     map[string]hclsyntax.Expression
	moduleCalls map[string]moduleCall
}

type moduleCall struct {
	source   string
	dir      string
	metadata map[string]string
}

type pendingReference struct {
	from      string
	scope     *moduleScope
	traversal hcl.Traversal
}

type moduleParser struct {
	parser      *hclparse.Parser
	graph       *model.Graph
	scopes      map[string]*moduleScope
	pending     []pendingReference
	outputCache map[string][]string
}

func parseHCLDir(dir string) (*model.Graph, error) {
	graph := model.New()
	graph.Groups = append(graph.Groups, model.Group{
		ID:     rootGroupID,
		Name:   "root",
		Kind:   model.GroupModule,
		Status: model.StatusUnchanged,
	})

	parser := &moduleParser{
		parser:      hclparse.NewParser(),
		graph:       graph,
		scopes:      make(map[string]*moduleScope),
		outputCache: make(map[string][]string),
	}
	if err := parser.parseModule(dir, "", rootGroupID, make(map[string]bool)); err != nil {
		return nil, err
	}
	if len(graph.Nodes) == 0 {
		return nil, fmt.Errorf("terraform: %s: no resource or data blocks found; add at least one resource", dir)
	}
	parser.collectEdges()

	graph.Sort()
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func (m *moduleParser) parseModule(dir, modulePath, groupID string, stack map[string]bool) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("terraform: %s: %w", dir, err)
	}
	if stack[absolute] {
		return fmt.Errorf("terraform: module cycle at %s; local module sources must not include themselves", dir)
	}
	stack[absolute] = true
	defer delete(stack, absolute)

	files, err := readHCLFiles(dir, m.parser)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("terraform: %s: no .tf files found; add Terraform configuration", dir)
	}

	scope := &moduleScope{
		path:        modulePath,
		dir:         dir,
		groupID:     groupID,
		types:       make(map[string]map[string]bool),
		dataTypes:   make(map[string]map[string]bool),
		outputs:     make(map[string]hclsyntax.Expression),
		moduleCalls: make(map[string]moduleCall),
	}
	m.scopes[modulePath] = scope

	var blocks []resourceBlock
	var dataBlocks []resourceBlock
	for _, file := range files {
		for _, block := range file.body.Blocks {
			switch block.Type {
			case "resource":
				resource, err := namedBlock(file.path, block, "resource")
				if err != nil {
					return err
				}
				if err := addResourceType(scope.types, resource); err != nil {
					return err
				}
				blocks = append(blocks, resource)
			case "data":
				resource, err := namedBlock(file.path, block, "data")
				if err != nil {
					return err
				}
				if err := addResourceType(scope.dataTypes, resource); err != nil {
					return err
				}
				dataBlocks = append(dataBlocks, resource)
			case "output":
				if len(block.Labels) != 1 {
					return fmt.Errorf("terraform: %s:%d: output block needs a name",
						file.path, block.TypeRange.Start.Line)
				}
				if value := block.Body.Attributes["value"]; value != nil {
					scope.outputs[block.Labels[0]] = value.Expr
				}
			case "module":
				if len(block.Labels) != 1 {
					return fmt.Errorf("terraform: %s:%d: module block needs a name",
						file.path, block.TypeRange.Start.Line)
				}
				source, ok := literalString(block.Body.Attributes["source"])
				if !ok || source == "" {
					return fmt.Errorf("terraform: %s:%d: module %q needs a literal source path",
						file.path, block.TypeRange.Start.Line, block.Labels[0])
				}
				if !localModuleSource(source) {
					continue
				}
				callDir := filepath.Clean(filepath.Join(dir, filepath.FromSlash(source)))
				if previous, exists := scope.moduleCalls[block.Labels[0]]; exists {
					return fmt.Errorf("terraform: %s:%d: module %q is declared twice (sources %s and %s)",
						file.path, block.TypeRange.Start.Line, block.Labels[0], previous.source, source)
				}
				scope.moduleCalls[block.Labels[0]] = moduleCall{
					source: source, dir: callDir, metadata: instanceMetadata(block.Body),
				}
			}
		}
	}

	for _, block := range blocks {
		id := resourceNodeID(modulePath, block.typeName, block.name)
		nodeType := infer.ResourceType(block.typeName)
		m.graph.Nodes = append(m.graph.Nodes, model.Node{
			ID:       id,
			Name:     block.name,
			Type:     nodeType,
			Group:    groupID,
			Source:   model.Source{File: block.file, Line: block.line},
			Metadata: redact.Metadata(blockMetadata(block)),
			Status:   model.StatusUnchanged,
		})
		m.collectReferences(id, scope, block)
	}
	for _, block := range dataBlocks {
		metadata := blockMetadata(block)
		metadata["data_source"] = "true"
		m.graph.Nodes = append(m.graph.Nodes, model.Node{
			ID:       dataNodeID(modulePath, block.typeName, block.name),
			Name:     block.name,
			Type:     infer.ResourceType(block.typeName),
			Group:    groupID,
			Source:   model.Source{File: block.file, Line: block.line},
			Metadata: redact.Metadata(metadata),
			Status:   model.StatusUnchanged,
		})
		m.collectReferences(dataNodeID(modulePath, block.typeName, block.name), scope, block)
	}

	names := make([]string, 0, len(scope.moduleCalls))
	for name := range scope.moduleCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		call := scope.moduleCalls[name]
		childPath := joinModulePath(modulePath, name)
		childGroupID := "group:module:" + childPath
		childMetadata := call.metadata
		if len(childMetadata) == 0 {
			childMetadata = nil
		}
		m.graph.Groups = append(m.graph.Groups, model.Group{
			ID:       childGroupID,
			Name:     childPath,
			Kind:     model.GroupModule,
			Metadata: childMetadata,
			Status:   model.StatusUnchanged,
		})
		if err := m.parseModule(call.dir, childPath, childGroupID, stack); err != nil {
			return err
		}
	}

	return nil
}

func readHCLFiles(dir string, parser *hclparse.Parser) ([]parsedFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("terraform: %s: %w", dir, err)
	}

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
	return files, nil
}

func namedBlock(path string, block *hclsyntax.Block, kind string) (resourceBlock, error) {
	if len(block.Labels) != 2 {
		return resourceBlock{}, fmt.Errorf("terraform: %s:%d: %s block needs a type and a name",
			path, block.TypeRange.Start.Line, kind)
	}
	return resourceBlock{
		typeName: block.Labels[0],
		name:     block.Labels[1],
		file:     path,
		line:     block.TypeRange.Start.Line,
		body:     block.Body,
	}, nil
}

func addResourceType(types map[string]map[string]bool, resource resourceBlock) error {
	if types[resource.typeName] == nil {
		types[resource.typeName] = make(map[string]bool)
	}
	if types[resource.typeName][resource.name] {
		return fmt.Errorf("terraform: %s:%d: %s %q is declared twice in this module",
			resource.file, resource.line, resource.typeName, resource.name)
	}
	types[resource.typeName][resource.name] = true
	return nil
}

func (m *moduleParser) collectReferences(from string, scope *moduleScope, block resourceBlock) {
	collector := &traversalCollector{}
	_ = hclsyntax.Walk(block.body, collector)
	for _, traversal := range collector.traversals {
		m.pending = append(m.pending, pendingReference{
			from:      from,
			scope:     scope,
			traversal: traversal,
		})
	}
}

func (m *moduleParser) collectEdges() {
	seen := make(map[string]bool)
	for _, reference := range m.pending {
		for _, target := range m.resolveTraversal(reference.scope, reference.traversal, make(map[string]bool)) {
			if target == reference.from {
				continue
			}
			key := reference.from + "->" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			m.graph.Edges = append(m.graph.Edges, model.Edge{
				From:   reference.from,
				To:     target,
				Kind:   model.EdgeReference,
				Status: model.StatusUnchanged,
			})
		}
	}
}

func (m *moduleParser) resolveTraversal(scope *moduleScope, traversal hcl.Traversal, seen map[string]bool) []string {
	if scope == nil || len(traversal) < 2 {
		return nil
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok {
		return nil
	}
	attribute, ok := traversal[1].(hcl.TraverseAttr)
	if !ok {
		return nil
	}

	switch root.Name {
	case "module":
		if len(traversal) < 3 {
			return nil
		}
		output, ok := traversal[2].(hcl.TraverseAttr)
		if !ok {
			return nil
		}
		child := m.scopes[joinModulePath(scope.path, attribute.Name)]
		if child == nil {
			return nil
		}
		return m.resolveOutput(child, output.Name, seen)
	case "data":
		if len(traversal) < 3 {
			return nil
		}
		name, ok := traversal[2].(hcl.TraverseAttr)
		if !ok || !scope.dataTypes[attribute.Name][name.Name] {
			return nil
		}
		return []string{dataNodeID(scope.path, attribute.Name, name.Name)}
	default:
		if !scope.types[root.Name][attribute.Name] {
			return nil
		}
		return []string{resourceNodeID(scope.path, root.Name, attribute.Name)}
	}
}

func (m *moduleParser) resolveOutput(scope *moduleScope, name string, seen map[string]bool) []string {
	key := scope.path + "\x00" + name
	if cached, ok := m.outputCache[key]; ok {
		return cached
	}
	if seen[key] {
		return nil
	}
	seen[key] = true
	expression := scope.outputs[name]
	if expression == nil {
		return nil
	}

	collector := &traversalCollector{}
	_ = hclsyntax.Walk(expression, collector)
	var targets []string
	for _, traversal := range collector.traversals {
		targets = append(targets, m.resolveTraversal(scope, traversal, seen)...)
	}
	targets = sortedUnique(targets)
	m.outputCache[key] = targets
	return targets
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
	if attribute := block.body.Attributes["tags"]; attribute != nil {
		if hash, ok := tagsHash(attribute.Expr); ok {
			metadata["tags_hash"] = hash
		}
	}
	for key, value := range instanceMetadata(block.body) {
		metadata[key] = value
	}
	collectNetworkBlocks(block.body, metadata)
	var topLevelCIDRs []string
	if attribute := block.body.Attributes["cidr_blocks"]; attribute != nil {
		if cidrs, ok := stringList(attribute.Expr); ok {
			topLevelCIDRs = append(topLevelCIDRs, cidrs...)
		}
	}
	for _, name := range []string{"cidr_ipv4", "cidr_ipv6"} {
		if attribute := block.body.Attributes[name]; attribute != nil {
			if value, ok := literalValue(attribute.Expr); ok {
				topLevelCIDRs = append(topLevelCIDRs, value)
			}
		}
	}
	if len(topLevelCIDRs) > 0 {
		metadata["cidrs"] = strings.Join(sortedUnique(topLevelCIDRs), ", ")
	}
	return metadata
}

func instanceMetadata(body *hclsyntax.Body) map[string]string {
	metadata := map[string]string{}
	if attribute := body.Attributes["count"]; attribute != nil {
		if value, ok := literalValue(attribute.Expr); ok {
			metadata["count"] = value
		}
	}
	if attribute := body.Attributes["for_each"]; attribute != nil {
		if keys, ok := collectionKeys(attribute.Expr); ok {
			metadata["for_each"] = strings.Join(keys, ", ")
		}
	}
	return metadata
}

func collectNetworkBlocks(body *hclsyntax.Body, metadata map[string]string) {
	for _, direction := range []string{"ingress", "egress"} {
		var cidrs []string
		var ports []string
		for _, block := range body.Blocks {
			if block.Type != direction {
				continue
			}
			for _, attributeName := range []string{"cidr_blocks", "ipv6_cidr_blocks"} {
				if attribute := block.Body.Attributes[attributeName]; attribute != nil {
					if values, ok := stringList(attribute.Expr); ok {
						cidrs = append(cidrs, values...)
					}
				}
			}
			if port, ok := portRange(block.Body); ok {
				ports = append(ports, port)
			}
		}
		if len(cidrs) > 0 {
			metadata[direction+"_cidrs"] = strings.Join(sortedUnique(cidrs), ", ")
		}
		if len(ports) > 0 {
			metadata[direction+"_ports"] = strings.Join(sortedUnique(ports), ", ")
		}
	}
}

func portRange(body *hclsyntax.Body) (string, bool) {
	from, fromOK := literalString(body.Attributes["from_port"])
	to, toOK := literalString(body.Attributes["to_port"])
	if !fromOK && !toOK {
		return "", false
	}
	protocol, _ := literalString(body.Attributes["protocol"])
	if protocol == "" {
		protocol = "tcp"
	}
	switch {
	case fromOK && toOK && from != to:
		return from + "-" + to + "/" + protocol, true
	case fromOK:
		return from + "/" + protocol, true
	default:
		return to + "/" + protocol, true
	}
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

func literalString(attribute *hclsyntax.Attribute) (string, bool) {
	if attribute == nil {
		return "", false
	}
	return literalValue(attribute.Expr)
}

func stringList(expression hclsyntax.Expression) ([]string, bool) {
	value, diags := expression.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() {
		return nil, false
	}
	if !value.CanIterateElements() {
		return nil, false
	}
	var values []string
	for iterator := value.ElementIterator(); iterator.Next(); {
		_, element := iterator.Element()
		if element.Type() != cty.String || !element.IsKnown() || element.IsNull() {
			return nil, false
		}
		values = append(values, element.AsString())
	}
	return sortedUnique(values), true
}

func collectionKeys(expression hclsyntax.Expression) ([]string, bool) {
	object, ok := expression.(*hclsyntax.ObjectConsExpr)
	if ok {
		var keys []string
		for _, item := range object.Items {
			if key, ok := objectKey(item.KeyExpr); ok {
				keys = append(keys, key)
			}
		}
		if len(keys) > 0 {
			return sortedUnique(keys), true
		}
	}
	return stringList(expression)
}

func tagsHash(expression hclsyntax.Expression) (string, bool) {
	object, ok := expression.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return "", false
	}
	pairs := make([]string, 0, len(object.Items))
	for _, item := range object.Items {
		key, ok := objectKey(item.KeyExpr)
		if !ok {
			continue
		}
		value, ok := literalValue(item.ValueExpr)
		if !ok {
			value = "<dynamic>"
		}
		pairs = append(pairs, key+"="+value)
	}
	if len(pairs) == 0 {
		return "", false
	}
	sort.Strings(pairs)
	digest := sha256.Sum256([]byte(strings.Join(pairs, "\n")))
	return hex.EncodeToString(digest[:8]), true
}

func objectKey(expression hclsyntax.Expression) (string, bool) {
	key, ok := expression.(*hclsyntax.ObjectConsKeyExpr)
	if !ok {
		return literalValue(expression)
	}
	if traversal, ok := key.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && len(traversal.Traversal) == 1 {
		if root, ok := traversal.Traversal[0].(hcl.TraverseRoot); ok {
			return root.Name, true
		}
	}
	if value, ok := literalValue(key.Wrapped); ok {
		return value, true
	}
	return literalValue(expression)
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

func localModuleSource(source string) bool {
	return strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || filepath.IsAbs(source)
}

func joinModulePath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func terraformPrefix(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	return "module." + modulePath + "."
}

func resourceNodeID(modulePath, resourceType, name string) string {
	return "tf:" + terraformPrefix(modulePath) + resourceType + "." + name
}

func dataNodeID(modulePath, dataType, name string) string {
	return "tf:" + terraformPrefix(modulePath) + "data." + dataType + "." + name
}

func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
