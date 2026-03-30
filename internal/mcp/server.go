package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/drkilla/legacy-map/internal/calltree"
	"github.com/drkilla/legacy-map/internal/watcher"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server wired to the given trace store.
func NewServer(store *watcher.Store) *server.MCPServer {
	s := server.NewMCPServer("legacy-map", "0.1.0")

	s.AddTool(toolGetLastTrace(), handleGetLastTrace(store))
	s.AddTool(toolGetTraceByURI(), handleGetTraceByURI(store))
	s.AddTool(toolListTraces(), handleListTraces(store))

	return s
}

// --- Tool definitions ---

func toolGetLastTrace() mcp.Tool {
	return mcp.Tool{
		Name:        "get_last_trace",
		Description: "Retourne le call tree des N dernières requêtes HTTP capturées par XDebug. Chaque trace contient l'arbre d'appels filtré (uniquement le code applicatif), les services impliqués et leurs rôles, les appels externes (Doctrine, etc), et la durée de chaque appel.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"n": map[string]any{
					"type":        "integer",
					"description": "Nombre de traces à retourner (défaut: 1, max: 20)",
					"default":     1,
				},
			},
		},
	}
}

func toolGetTraceByURI() mcp.Tool {
	return mcp.Tool{
		Name:        "get_trace_by_uri",
		Description: "Retourne les traces capturées qui matchent une URI donnée (recherche partielle). Ex: get_trace_by_uri('/reservations') retourne toutes les traces dont l'URI contient '/reservations'.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"uri": map[string]any{
					"type":        "string",
					"description": "Fragment d'URI à rechercher",
				},
			},
			Required: []string{"uri"},
		},
	}
}

func toolListTraces() mcp.Tool {
	return mcp.Tool{
		Name:        "list_traces",
		Description: "Liste toutes les traces disponibles en mémoire avec leurs métadonnées (timestamp, URI, méthode HTTP, nombre d'appels). N'inclut pas le call tree complet — utilisez get_last_trace ou get_trace_by_uri pour le détail.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
		},
	}
}

// --- Handlers ---

func handleGetLastTrace(store *watcher.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := req.GetInt("n", 1)
		if n < 1 {
			n = 1
		}
		if n > 20 {
			n = 20
		}

		traces := store.Last(n)
		if len(traces) == 0 {
			return textResult("Aucune trace disponible. Exécutez une action dans le navigateur pour générer une trace XDebug."), nil
		}

		return jsonResult(traces)
	}
}

func handleGetTraceByURI(store *watcher.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		uri, err := req.RequireString("uri")
		if err != nil {
			return nil, err
		}

		all := store.All()
		var matches []*calltree.TraceResult
		for _, t := range all {
			if strings.Contains(t.URI, uri) {
				matches = append(matches, t)
			}
		}

		if len(matches) == 0 {
			return textResult("Aucune trace trouvée pour l'URI '" + uri + "'."), nil
		}

		return jsonResult(matches)
	}
}

func handleListTraces(store *watcher.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		all := store.All()
		if len(all) == 0 {
			return textResult("Aucune trace disponible."), nil
		}

		type traceSummary struct {
			Timestamp     string  `json:"timestamp"`
			HTTPMethod    string  `json:"http_method,omitempty"`
			URI           string  `json:"uri,omitempty"`
			TotalCallsRaw int    `json:"total_calls_raw"`
			FilteredCalls int    `json:"total_calls_filtered"`
			DurationMs    float64 `json:"duration_ms"`
			TraceFile     string  `json:"trace_file"`
		}

		summaries := make([]traceSummary, len(all))
		for i, t := range all {
			summaries[i] = traceSummary{
				Timestamp:     t.Timestamp,
				HTTPMethod:    t.HTTPMethod,
				URI:           t.URI,
				TotalCallsRaw: t.TotalCalls,
				FilteredCalls: t.FilteredCalls,
				DurationMs:    t.DurationMs,
				TraceFile:     t.TraceFile,
			}
		}

		return jsonResult(summaries)
	}
}

// --- Helpers ---

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: text},
		},
	}
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: string(b)},
		},
	}, nil
}
