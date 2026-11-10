package render

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/nonomnonom/infradiff/internal/model"
)

// JSON renders the documented graph schema.
func JSON(graph *model.Graph) ([]byte, error) {
	g := prepared(graph)

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(g); err != nil {
		return nil, fmt.Errorf("render json: %w", err)
	}
	return buf.Bytes(), nil
}
