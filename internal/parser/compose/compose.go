// Package compose parses Docker Compose files into infrastructure graphs.
package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/parser/infer"
	"github.com/nonomnonom/infradiff/internal/redact"
	"gopkg.in/yaml.v3"
)

// Parser implements parser.Parser for Docker Compose.
type Parser struct{}

// New returns a Compose parser.
func New() *Parser { return &Parser{} }

// Name returns the parser name used in diagnostics.
func (p *Parser) Name() string { return "docker-compose" }

// Detect reports whether path is a Compose file. Well-known file names are
// always claimed so that syntax errors surface as Compose errors; other YAML
// files are claimed only when they contain a services mapping.
func (p *Parser) Detect(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	}
	root, err := readDocument(path)
	if err != nil {
		return false
	}
	services := mappingValue(root, "services")
	return services != nil && services.Kind == yaml.MappingNode
}

// Parse reads one Compose file.
func (p *Parser) Parse(path string) (*model.Graph, error) {
	root, err := readDocument(path)
	if err != nil {
		return nil, fmt.Errorf("compose: %s: %w", path, err)
	}
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("compose: %s: top level must be a mapping", path)
	}

	servicesNode := mappingValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode || len(servicesNode.Content) == 0 {
		return nil, lineError(path, root.Line, "no services defined; add a services: mapping")
	}

	project := scalarValue(root, "name")
	if project == "" {
		project = projectName(path)
	}
	groupID := "group:project:" + project

	graph := model.New()
	graph.Groups = append(graph.Groups, model.Group{
		ID:     groupID,
		Name:   project,
		Kind:   model.GroupProject,
		Status: model.StatusUnchanged,
	})

	serviceEntries := mappingEntries(servicesNode)
	networksNode := mappingValue(root, "networks")
	volumesNode := mappingValue(root, "volumes")
	declaredNetworks := mappingKeys(networksNode)
	declaredVolumes := mappingKeys(volumesNode)

	specs := make(map[string]serviceSpec, len(serviceEntries))
	defaultNetworkNeeded := false
	for _, entry := range serviceEntries {
		spec, err := parseService(path, entry.name, entry.line, entry.value)
		if err != nil {
			return nil, err
		}
		specs[entry.name] = spec
		if len(spec.networks) == 0 {
			defaultNetworkNeeded = true
		}
	}

	networkSet := make(map[string]bool, len(declaredNetworks)+1)
	for _, network := range declaredNetworks {
		networkSet[network] = true
	}
	if defaultNetworkNeeded || (len(declaredNetworks) == 0 && len(serviceEntries) > 0) || usesNetwork(specs, "default") {
		networkSet["default"] = true
	}

	for name, spec := range specs {
		for _, dependency := range spec.dependsOn {
			if _, ok := specs[dependency]; !ok {
				return nil, lineError(path, spec.line,
					"service %q: depends_on refers to undefined service %q", name, dependency)
			}
		}
		for _, network := range spec.networks {
			if !networkSet[network] {
				return nil, lineError(path, spec.line,
					"service %q: network %q is not declared under top-level networks", name, network)
			}
		}
		for _, volume := range spec.namedVolumes {
			if !contains(declaredVolumes, volume) {
				return nil, lineError(path, spec.line,
					"service %q: volume %q is not declared under top-level volumes", name, volume)
			}
		}
	}

	for _, name := range sortedKeys(specs) {
		spec := specs[name]
		metadata := map[string]string{}
		if spec.image != "" {
			metadata["image"] = spec.image
		}
		if len(spec.ports) > 0 {
			metadata["ports"] = strings.Join(spec.ports, ", ")
		}
		if len(spec.envNames) > 0 {
			metadata["environment"] = strings.Join(spec.envNames, ", ")
		}
		nodeType := infer.ImageType(spec.image)
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       "compose:service:" + name,
			Name:     name,
			Type:     nodeType,
			Group:    groupID,
			Source:   model.Source{File: path, Line: spec.line},
			Metadata: redact.Metadata(metadata),
			Status:   model.StatusUnchanged,
		})
	}

	for _, entry := range mappingEntries(networksNode) {
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       "compose:network:" + entry.name,
			Name:     entry.name,
			Type:     model.NodeNetwork,
			Group:    groupID,
			Source:   model.Source{File: path, Line: entry.line},
			Metadata: map[string]string{},
			Status:   model.StatusUnchanged,
		})
	}
	if networkSet["default"] && !contains(declaredNetworks, "default") {
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       "compose:network:default",
			Name:     "default",
			Type:     model.NodeNetwork,
			Group:    groupID,
			Source:   model.Source{File: path, Line: servicesNode.Line},
			Metadata: map[string]string{"implicit": "compose default network"},
			Status:   model.StatusUnchanged,
		})
	}

	for _, entry := range mappingEntries(volumesNode) {
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       "compose:volume:" + entry.name,
			Name:     entry.name,
			Type:     model.NodeStorage,
			Group:    groupID,
			Source:   model.Source{File: path, Line: entry.line},
			Metadata: map[string]string{},
			Status:   model.StatusUnchanged,
		})
	}

	for _, name := range sortedKeys(specs) {
		spec := specs[name]
		serviceID := "compose:service:" + name
		networkKind := model.EdgeNetwork
		if len(spec.ports) > 0 {
			networkKind = model.EdgePort
		}
		for _, network := range spec.networksOrDefault(networkSet) {
			graph.Edges = append(graph.Edges, model.Edge{
				From:   serviceID,
				To:     "compose:network:" + network,
				Kind:   networkKind,
				Status: model.StatusUnchanged,
			})
		}
		for _, dependency := range spec.dependsOn {
			graph.Edges = append(graph.Edges, model.Edge{
				From:   serviceID,
				To:     "compose:service:" + dependency,
				Kind:   model.EdgeDependsOn,
				Status: model.StatusUnchanged,
			})
		}
		for _, volume := range spec.namedVolumes {
			graph.Edges = append(graph.Edges, model.Edge{
				From:   serviceID,
				To:     "compose:volume:" + volume,
				Kind:   model.EdgeVolume,
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

type serviceSpec struct {
	image        string
	dependsOn    []string
	networks     []string
	namedVolumes []string
	ports        []string
	envNames     []string
	line         int
}

func (s serviceSpec) networksOrDefault(declared map[string]bool) []string {
	if len(s.networks) > 0 {
		return s.networks
	}
	if declared["default"] {
		return []string{"default"}
	}
	return nil
}

func parseService(path, name string, line int, node *yaml.Node) (serviceSpec, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return serviceSpec{}, lineError(path, line,
			"service %q: definition must be a mapping", name)
	}

	spec := serviceSpec{line: line}
	if value := mappingValue(node, "image"); value != nil {
		spec.image = value.Value
	}

	var err error
	if value := mappingValue(node, "depends_on"); value != nil {
		spec.dependsOn, err = nameList(path, value, name, "depends_on")
		if err != nil {
			return serviceSpec{}, err
		}
	}
	if value := mappingValue(node, "networks"); value != nil {
		spec.networks, err = nameList(path, value, name, "networks")
		if err != nil {
			return serviceSpec{}, err
		}
	}
	if value := mappingValue(node, "volumes"); value != nil {
		spec.namedVolumes, err = volumeSources(path, value, name)
		if err != nil {
			return serviceSpec{}, err
		}
	}
	if value := mappingValue(node, "ports"); value != nil {
		spec.ports, err = portList(path, value, name)
		if err != nil {
			return serviceSpec{}, err
		}
	}
	if value := mappingValue(node, "environment"); value != nil {
		spec.envNames, err = environmentNames(path, value, name)
		if err != nil {
			return serviceSpec{}, err
		}
	}

	spec.dependsOn = sortedUnique(spec.dependsOn)
	spec.networks = sortedUnique(spec.networks)
	spec.namedVolumes = sortedUnique(spec.namedVolumes)
	return spec, nil
}

func nameList(path string, node *yaml.Node, service, field string) ([]string, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		var names []string
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode || item.Value == "" {
				return nil, lineError(path, item.Line,
					"service %q: %s entries must be names", service, field)
			}
			names = append(names, item.Value)
		}
		return names, nil
	case yaml.MappingNode:
		return mappingKeys(node), nil
	default:
		return nil, lineError(path, node.Line,
			"service %q: %s must be a list or mapping", service, field)
	}
}

func volumeSources(path string, node *yaml.Node, service string) ([]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, lineError(path, node.Line, "service %q: volumes must be a list", service)
	}
	var named []string
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			if source, ok := namedVolume(item.Value); ok {
				named = append(named, source)
			}
		case yaml.MappingNode:
			source := scalarValue(item, "source")
			volumeType := scalarValue(item, "type")
			if source == "" || isBindSource(source) {
				continue
			}
			if volumeType == "" || volumeType == "volume" {
				named = append(named, source)
			}
		default:
			return nil, lineError(path, item.Line,
				"service %q: volume entries must be strings or mappings", service)
		}
	}
	return named, nil
}

// namedVolume returns the source of a short-syntax volume when it names a
// Docker volume rather than a host path.
func namedVolume(value string) (string, bool) {
	if isWindowsDrivePath(value) || strings.HasPrefix(value, `\\`) {
		return "", false
	}
	source, _, found := strings.Cut(value, ":")
	if !found || source == "" || isBindSource(source) {
		return "", false
	}
	return source, true
}

func isBindSource(source string) bool {
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") ||
		strings.HasPrefix(source, "~") || strings.HasPrefix(source, `\\`) {
		return true
	}
	return isWindowsDrivePath(source)
}

func isWindowsDrivePath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	letter := path[0]
	return letter >= 'a' && letter <= 'z' || letter >= 'A' && letter <= 'Z'
}

func portList(path string, node *yaml.Node, service string) ([]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, lineError(path, node.Line, "service %q: ports must be a list", service)
	}
	var ports []string
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			if item.Value != "" {
				ports = append(ports, item.Value)
			}
		case yaml.MappingNode:
			published := scalarValue(item, "published")
			if published == "" {
				published = scalarValue(item, "target")
			}
			if published != "" {
				ports = append(ports, published)
			}
		default:
			return nil, lineError(path, item.Line,
				"service %q: port entries must be strings or mappings", service)
		}
	}
	return sortedUnique(ports), nil
}

func environmentNames(path string, node *yaml.Node, service string) ([]string, error) {
	var names []string
	switch node.Kind {
	case yaml.MappingNode:
		for _, entry := range mappingEntries(node) {
			names = append(names, entry.name)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, lineError(path, item.Line,
					"service %q: environment entries must be KEY=value strings", service)
			}
			name, _, _ := strings.Cut(item.Value, "=")
			if name != "" {
				names = append(names, name)
			}
		}
	default:
		return nil, lineError(path, node.Line,
			"service %q: environment must be a list or mapping", service)
	}
	return sortedUnique(names), nil
}

func readDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	return document.Content[0], nil
}

func projectName(path string) string {
	dir := filepath.Base(filepath.Dir(path))
	if dir == "." || dir == string(filepath.Separator) || dir == "" {
		return "project"
	}
	return dir
}

func lineError(path string, line int, format string, args ...any) error {
	return fmt.Errorf("compose: %s:%d: %s", path, line, fmt.Sprintf(format, args...))
}

type mappingEntry struct {
	name  string
	line  int
	value *yaml.Node
}

func mappingEntries(node *yaml.Node) []mappingEntry {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	entries := make([]mappingEntry, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		entries = append(entries, mappingEntry{
			name:  node.Content[i].Value,
			line:  node.Content[i].Line,
			value: node.Content[i+1],
		})
	}
	return entries
}

func mappingKeys(node *yaml.Node) []string {
	entries := mappingEntries(node)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.name)
	}
	sort.Strings(names)
	return names
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func scalarValue(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func usesNetwork(specs map[string]serviceSpec, want string) bool {
	for _, spec := range specs {
		for _, network := range spec.networks {
			if network == want {
				return true
			}
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func sortedKeys(specs map[string]serviceSpec) []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
