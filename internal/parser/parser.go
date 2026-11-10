package parser

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/infradiff/infradiff/internal/model"
	"github.com/infradiff/infradiff/internal/parser/compose"
	"github.com/infradiff/infradiff/internal/parser/kubernetes"
	"github.com/infradiff/infradiff/internal/parser/terraform"
)

// Parser turns one file or directory into a graph.
type Parser interface {
	Name() string
	Detect(path string) bool
	Parse(path string) (*model.Graph, error)
}

// DirParser is implemented by formats whose files reference each other. When a
// directory contains several such files, the registry calls ParseDir once and
// merges the single resulting graph.
type DirParser interface {
	ParseDir(dir string) (*model.Graph, error)
}

// Registry detects the format of each input and merges the resulting graphs.
type Registry struct {
	parsers []Parser
}

// NewRegistry returns a registry that tries parsers in the given order.
func NewRegistry(parsers ...Parser) *Registry {
	return &Registry{parsers: parsers}
}

// DefaultRegistry returns the built-in parsers.
func DefaultRegistry() *Registry {
	return NewRegistry(compose.New(), terraform.New(), kubernetes.New())
}

// ParserFor returns the first parser that recognizes path.
func (r *Registry) ParserFor(path string) (Parser, bool) {
	for _, p := range r.parsers {
		if p.Detect(path) {
			return p, true
		}
	}
	return nil, false
}

// ParsePath parses a file or a directory tree and returns one merged graph.
func (r *Registry) ParsePath(path string) (*model.Graph, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("infradiff: cannot read %s: %w", path, err)
	}

	if !info.IsDir() {
		return r.parseOneFile(path)
	}
	return r.parseDir(path)
}

func (r *Registry) parseOneFile(path string) (*model.Graph, error) {
	p, ok := r.ParserFor(path)
	if !ok {
		return nil, fmt.Errorf("%s: unknown infrastructure format; supported formats: %s", path, r.supported())
	}
	graph, err := p.Parse(path)
	if err != nil {
		return nil, err
	}
	rewriteSources(graph, filepath.Dir(path))
	return finish(graph)
}

func (r *Registry) parseDir(root string) (*model.Graph, error) {
	files, err := collectFiles(root)
	if err != nil {
		return nil, err
	}

	graphs := make([]*model.Graph, 0, len(files))
	handledDirs := make(map[string]bool)
	for _, file := range files {
		p, ok := r.ParserFor(file)
		if !ok {
			continue
		}

		var graph *model.Graph
		if dirParser, ok := p.(DirParser); ok && usesDirectoryGrouping(file) {
			dir := filepath.Dir(file)
			key := p.Name() + "\x00" + dir
			if handledDirs[key] {
				continue
			}
			handledDirs[key] = true
			graph, err = dirParser.ParseDir(dir)
		} else {
			graph, err = p.Parse(file)
		}
		if err != nil {
			return nil, err
		}
		rewriteSources(graph, root)
		graphs = append(graphs, graph)
	}
	if len(graphs) == 0 {
		return nil, fmt.Errorf("%s: no supported infrastructure files found (supported formats: %s)", root, r.supported())
	}

	merged, err := Merge(graphs...)
	if err != nil {
		return nil, err
	}
	return finish(merged)
}

func finish(graph *model.Graph) (*model.Graph, error) {
	graph.Sort()
	graph.Recount()
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func (r *Registry) supported() string {
	names := make([]string, 0, len(r.parsers))
	for _, p := range r.parsers {
		names = append(names, p.Name())
	}
	return strings.Join(names, ", ")
}

var skippedDirs = map[string]bool{
	".git":         true,
	".terraform":   true,
	".venv":        true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
}

func collectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("infradiff: walking %s: %w", root, err)
		}
		if entry.IsDir() {
			if path != root && skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// usesDirectoryGrouping limits ParseDir to formats where one directory is one
// logical unit. Plan JSON files are single-file inputs even though the
// Terraform parser also implements DirParser.
func usesDirectoryGrouping(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".tf", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// rewriteSources makes every source path relative to the scanned root and
// slash-separated, so identical inputs produce identical output on every OS.
func rewriteSources(graph *model.Graph, root string) {
	for i := range graph.Nodes {
		source := graph.Nodes[i].Source.File
		if source == "" {
			continue
		}
		rel, err := filepath.Rel(root, source)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = source
		}
		graph.Nodes[i].Source.File = filepath.ToSlash(rel)
	}
}

// Merge combines graphs and rejects conflicting definitions of the same node,
// edge, or group. Identical duplicates are collapsed.
func Merge(graphs ...*model.Graph) (*model.Graph, error) {
	out := model.New()
	groupIndex := make(map[string]model.Group)
	nodeIndex := make(map[string]model.Node)
	type edgeKey struct{ from, to string }
	edgeIndex := make(map[edgeKey]model.Edge)

	for _, graph := range graphs {
		if graph == nil {
			continue
		}
		for _, group := range graph.Groups {
			if existing, ok := groupIndex[group.ID]; ok {
				if !reflect.DeepEqual(existing, group) {
					return nil, fmt.Errorf(
						"merge: group %q is defined twice with different content", group.ID)
				}
				continue
			}
			groupIndex[group.ID] = group
			out.Groups = append(out.Groups, group)
		}
		for _, node := range graph.Nodes {
			if existing, ok := nodeIndex[node.ID]; ok {
				if !sameNode(existing, node) {
					return nil, fmt.Errorf(
						"merge: node %q is defined twice with different content (%s and %s); give one definition a unique name",
						node.ID, existing.Source.File, node.Source.File)
				}
				continue
			}
			nodeIndex[node.ID] = node
			out.Nodes = append(out.Nodes, node)
		}
		for _, edge := range graph.Edges {
			key := edgeKey{edge.From, edge.To}
			if existing, ok := edgeIndex[key]; ok {
				if existing != edge {
					return nil, fmt.Errorf(
						"merge: conflicting edges %q -> %q (%s and %s); a node pair can have only one edge",
						edge.From, edge.To, existing.Kind, edge.Kind)
				}
				continue
			}
			edgeIndex[key] = edge
			out.Edges = append(out.Edges, edge)
		}
	}
	return out, nil
}

func sameNode(a, b model.Node) bool {
	return a.ID == b.ID &&
		a.Name == b.Name &&
		a.Type == b.Type &&
		a.Group == b.Group &&
		a.Source == b.Source &&
		a.Status == b.Status &&
		reflect.DeepEqual(a.Metadata, b.Metadata)
}
