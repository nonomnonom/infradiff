package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
)

const mermaidVersion = "12.3.0"

//go:embed assets/viewer.html
var viewerSource string

//go:embed assets/mermaid.min.js
var mermaidJS []byte

//go:embed assets/mermaid.LICENSE
var mermaidLicense string

var viewer = template.Must(template.New("viewer").Parse(viewerSource))

type viewerData struct {
	Diagram        string
	Summary        string
	MermaidJS      template.JS
	MermaidVersion string
	License        string
}

// HTML renders one self-contained page: the Mermaid runtime, the diagram, a
// status legend, and pan/zoom controls. It never loads anything from the
// network.
func HTML(graph *model.Graph) ([]byte, error) {
	g := prepared(graph)
	script := strings.ReplaceAll(string(mermaidJS), "</script", `<\/script`)

	var buf bytes.Buffer
	err := viewer.Execute(&buf, viewerData{
		Diagram:        string(Mermaid(g)),
		Summary:        g.Summary.Text,
		MermaidJS:      template.JS(script),
		MermaidVersion: mermaidVersion,
		License:        mermaidLicense,
	})
	if err != nil {
		return nil, fmt.Errorf("render html: %w", err)
	}
	return buf.Bytes(), nil
}
