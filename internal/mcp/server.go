package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	s.AddTool(toolTriggerTrace(), handleTriggerTrace(store))

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

func toolTriggerTrace() mcp.Tool {
	return mcp.Tool{
		Name:        "trigger_trace",
		Description: "Déclenche une requête HTTP vers une URL avec XDebug tracing activé, attend la trace, et retourne le call tree filtré. Permet d'analyser un endpoint sans quitter Claude Code. Exemple : trigger_trace({url: 'http://localhost:8000/api/clients', method: 'POST', body: '{\"name\":\"test\"}'})",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "URL complète à appeler (ex: http://localhost:8000/api/clients)",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "Méthode HTTP (défaut: GET)",
					"default":     "GET",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Corps de la requête (pour POST/PUT/PATCH)",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": "Headers HTTP additionnels (ex: {\"Authorization\": \"Bearer xxx\", \"Content-Type\": \"application/json\"})",
					"additionalProperties": map[string]any{
						"type": "string",
					},
				},
			},
			Required: []string{"url"},
		},
	}
}

func handleTriggerTrace(store *watcher.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawURL, err := req.RequireString("url")
		if err != nil {
			return nil, err
		}

		method := req.GetString("method", "GET")
		method = strings.ToUpper(method)
		body := req.GetString("body", "")

		// Parse and add XDEBUG_TRACE trigger to URL
		u, err := url.Parse(rawURL)
		if err != nil {
			return textResult(fmt.Sprintf("URL invalide : %s", err)), nil
		}
		q := u.Query()
		q.Set("XDEBUG_TRACE", "1")
		u.RawQuery = q.Encode()

		// Record store count before request
		countBefore := store.Count()

		// Build HTTP request
		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}
		httpReq, err := http.NewRequest(method, u.String(), bodyReader)
		if err != nil {
			return textResult(fmt.Sprintf("Erreur création requête : %s", err)), nil
		}

		// Add XDebug triggers (cover all modes)
		httpReq.AddCookie(&http.Cookie{Name: "XDEBUG_TRACE", Value: "1"})
		httpReq.Header.Set("X-Xdebug-Trigger", "1")

		// Add user headers
		if args := req.GetArguments(); args != nil {
			if headersRaw, ok := args["headers"]; ok {
				if headersMap, ok := headersRaw.(map[string]any); ok {
					for k, v := range headersMap {
						if vs, ok := v.(string); ok {
							httpReq.Header.Set(k, vs)
						}
					}
				}
			}
		}

		// Execute HTTP request
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return textResult(fmt.Sprintf("Erreur HTTP : %s", err)), nil
		}
		resp.Body.Close()

		statusCode := resp.StatusCode

		// Wait for trace to appear (max 15s)
		waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		trace, ok := store.WaitForNew(waitCtx, countBefore)
		if !ok {
			return textResult(fmt.Sprintf(
				"La requête %s %s a retourné le status %d, mais aucune trace XDebug n'a été capturée dans les 15 secondes.\n\n"+
					"Vérifications :\n"+
					"  - XDebug est installé et activé (xdebug.mode=trace)\n"+
					"  - xdebug.start_with_request=trigger est configuré\n"+
					"  - Le trace_output_dir pointe vers le dossier surveillé par legacy-map\n"+
					"  - Le serveur PHP a été redémarré après la configuration",
				method, rawURL, statusCode,
			)), nil
		}

		// Return trace with request context
		type triggerResult struct {
			HTTPStatus int                   `json:"http_status"`
			Request    string                `json:"request"`
			Trace      *calltree.TraceResult `json:"trace"`
		}

		return jsonResult(triggerResult{
			HTTPStatus: statusCode,
			Request:    fmt.Sprintf("%s %s", method, rawURL),
			Trace:      trace,
		})
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
