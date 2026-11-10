// Package terraform parses Terraform HCL files and terraform show -json plans.
package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
)

// Parser implements parser.Parser for Terraform.
type Parser struct{}

// New returns a Terraform parser.
func New() *Parser { return &Parser{} }

// Name returns the parser name used in diagnostics.
func (p *Parser) Name() string { return "terraform" }

// Detect reports whether path is a .tf file or a Terraform plan JSON file.
func (p *Parser) Detect(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tf":
		return true
	case ".json":
		return isPlanJSON(path)
	default:
		return false
	}
}

// ParseDir parses one HCL module directory.
func (p *Parser) ParseDir(dir string) (*model.Graph, error) {
	return parseHCLDir(dir)
}

// Parse reads an HCL module directory or a plan JSON file. Passing a .tf file
// parses its whole directory, because Terraform references span files.
func (p *Parser) Parse(path string) (*model.Graph, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("terraform: %s: %w", path, err)
	}

	if info.IsDir() {
		return parseHCLDir(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tf":
		return parseHCLDir(filepath.Dir(path))
	case ".json":
		return parsePlan(path)
	default:
		return nil, fmt.Errorf("terraform: %s: unsupported file type; use .tf files or terraform show -json output", path)
	}
}

func isPlanJSON(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var probe struct {
		FormatVersion string            `json:"format_version"`
		TerraformVer  string            `json:"terraform_version"`
		Changes       []json.RawMessage `json:"resource_changes"`
		PlannedValues json.RawMessage   `json:"planned_values"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	if probe.FormatVersion == "" || probe.TerraformVer == "" {
		return false
	}
	return probe.Changes != nil || probe.PlannedValues != nil
}
