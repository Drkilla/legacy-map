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
// httpTimeout is the default HTTP timeout for trigger_trace (in seconds).
func NewServer(store *watcher.Store, httpTimeout int) *server.MCPServer {
	s := server.NewMCPServer("legacy-map", "0.1.0")

	s.AddTool(toolGetLastTrace(), handleGetLastTrace(store))
	s.AddTool(toolGetTraceByURI(), handleGetTraceByURI(store))
	s.AddTool(toolListTraces(), handleListTraces(store))
	s.AddTool(toolTriggerTrace(), handleTriggerTrace(store, httpTimeout))

	return s
}

// --- Tool definitions ---

func toolGetLastTrace() mcp.Tool {
	return mcp.Tool{
		Name: "get_last_trace",
		Description: `Get the full call tree of the N most recent XDebug traces. Each trace shows the complete filtered execution flow: which controllers, services, repositories, and entities were called, in what order, with parameters and return values.

Use this after trigger_trace or to review recently captured traces.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"n": map[string]any{
					"type":        "integer",
					"description": "Number of traces to return (default: 1, max: 20)",
					"default":     1,
				},
			},
		},
	}
}

func toolGetTraceByURI() mcp.Tool {
	return mcp.Tool{
		Name: "get_trace_by_uri",
		Description: `Find traces matching a specific URI pattern. Use this to retrieve traces for a specific endpoint without triggering a new request.

Example: get_trace_by_uri("/clients") returns all traces whose URI contains "/clients".`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"uri": map[string]any{
					"type":        "string",
					"description": "URI fragment to search for",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of traces to return (default: 1)",
					"default":     1,
				},
			},
			Required: []string{"uri"},
		},
	}
}

func toolListTraces() mcp.Tool {
	return mcp.Tool{
		Name: "list_traces",
		Description: `List all XDebug traces currently in memory with metadata (URI, HTTP method, duration, call counts).

USE THIS TOOL FIRST when the user asks about application flows, endpoint behavior, or wants to understand what the app does. Check if relevant traces already exist before triggering new ones.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
		},
	}
}

func toolTriggerTrace() mcp.Tool {
	return mcp.Tool{
		Name: "trigger_trace",
		Description: `Trace a real HTTP request through the PHP/Symfony application using XDebug runtime capture.

USE THIS TOOL whenever the user asks:
- "what happens when I call/POST/GET [endpoint]"
- "trace the flow of [endpoint]"
- "retrace moi ce qui se passe sur [endpoint]"
- "show me the execution path of [request]"
- "qu'est-ce qui se passe quand [action]"
- any question about runtime behavior, execution flow, or what code is actually called

This gives the REAL execution flow captured at runtime, not static code analysis.
The result is filtered to show only application code (App\ namespace), with framework internals collapsed.

Parameters:
- url: Full URL to call (e.g. http://localhost:8000/api/clients)
- method: HTTP method (default: GET)
- body: Request body for POST/PUT/PATCH
- headers: Additional headers (e.g. Authorization Bearer token)
- timeout: HTTP timeout in seconds (default: 30)`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "Full URL to call (e.g. http://localhost:8000/api/clients)",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method (default: GET)",
					"default":     "GET",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Request body for POST/PUT/PATCH",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": `Additional HTTP headers (e.g. {"Authorization": "Bearer xxx", "Content-Type": "application/json"})`,
					"additionalProperties": map[string]any{
						"type": "string",
					},
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "HTTP timeout in seconds (default: 30). Increase for slow endpoints (LLM calls, heavy processing).",
				},
			},
			Required: []string{"url"},
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
			return textResult("No traces available. Trigger a request with ?XDEBUG_TRACE=1 or use trigger_trace to capture one."), nil
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
		limit := req.GetInt("limit", 1)
		if limit < 1 {
			limit = 1
		}

		all := store.All()
		var matches []*calltree.TraceResult
		for _, t := range all {
			if strings.Contains(t.URI, uri) {
				matches = append(matches, t)
			}
		}

		if len(matches) == 0 {
			return textResult("No traces found matching URI '" + uri + "'."), nil
		}

		totalMatches := len(matches)
		if limit < totalMatches {
			matches = matches[:limit]
		}

		type searchResult struct {
			Traces       []*calltree.TraceResult `json:"traces"`
			TotalMatches int                     `json:"total_matches"`
			Returned     int                     `json:"returned"`
			Note         string                  `json:"note,omitempty"`
		}

		result := searchResult{
			Traces:       matches,
			TotalMatches: totalMatches,
			Returned:     len(matches),
		}
		if totalMatches > limit {
			result.Note = fmt.Sprintf("%d other traces match this URI. Use limit=%d to see all.", totalMatches-limit, totalMatches)
		}

		return jsonResult(result)
	}
}

func handleListTraces(store *watcher.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		all := store.All()
		if len(all) == 0 {
			return textResult("No traces available."), nil
		}

		type traceSummary struct {
			Timestamp     string  `json:"timestamp"`
			HTTPMethod    string  `json:"http_method,omitempty"`
			URI           string  `json:"uri,omitempty"`
			TotalCallsRaw int     `json:"total_calls_raw"`
			FilteredCalls int     `json:"total_calls_filtered"`
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

func handleTriggerTrace(store *watcher.Store, defaultTimeout int) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawURL, err := req.RequireString("url")
		if err != nil {
			return nil, err
		}

		method := req.GetString("method", "GET")
		method = strings.ToUpper(method)
		body := req.GetString("body", "")

		timeout := req.GetInt("timeout", defaultTimeout)
		if timeout < 1 {
			timeout = defaultTimeout
		}

		// Parse and add XDEBUG_TRACE trigger to URL
		u, err := url.Parse(rawURL)
		if err != nil {
			return textResult(fmt.Sprintf("Invalid URL: %s", err)), nil
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
			return textResult(fmt.Sprintf("Request creation error: %s", err)), nil
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
		client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
		resp, err := client.Do(httpReq)

		httpTimedOut := false
		statusCode := 0
		if err != nil {
			if isTimeoutError(err) {
				httpTimedOut = true
			} else {
				return textResult(fmt.Sprintf("HTTP error: %s", err)), nil
			}
		} else {
			statusCode = resp.StatusCode
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		// Wait for trace to appear (max 15s, or 5s extra after HTTP timeout)
		waitDuration := 15 * time.Second
		if httpTimedOut {
			waitDuration = 5 * time.Second
		}
		waitCtx, cancel := context.WithTimeout(ctx, waitDuration)
		defer cancel()

		trace, ok := store.WaitForNew(waitCtx, countBefore)

		if httpTimedOut && ok {
			// HTTP timed out but trace was captured
			type triggerResult struct {
				HTTPStatus int                   `json:"http_status"`
				Request    string                `json:"request"`
				Warning    string                `json:"warning"`
				Trace      *calltree.TraceResult `json:"trace"`
			}
			return jsonResult(triggerResult{
				HTTPStatus: 0,
				Request:    fmt.Sprintf("%s %s", method, rawURL),
				Warning:    fmt.Sprintf("HTTP request timed out after %ds, but a trace was captured.", timeout),
				Trace:      trace,
			})
		}

		if httpTimedOut && !ok {
			return textResult(fmt.Sprintf(
				"HTTP request %s %s timed out after %ds and no trace was captured.\n\n"+
					"Increase the timeout: use timeout=%d or start the server with --http-timeout=%d",
				method, rawURL, timeout, timeout*2, timeout*2,
			)), nil
		}

		if !ok {
			return textResult(fmt.Sprintf(
				"Request %s %s returned status %d, but no XDebug trace was captured within %ds.\n\n"+
					"Checklist:\n"+
					"  - XDebug is installed and enabled (xdebug.mode=trace)\n"+
					"  - xdebug.start_with_request=trigger is configured\n"+
					"  - xdebug.use_compression=0 is set (legacy-map cannot read .xt.gz files)\n"+
					"  - trace_output_dir points to the directory watched by legacy-map\n"+
					"  - PHP was restarted after configuration changes",
				method, rawURL, statusCode, int(waitDuration.Seconds()),
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

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Client.Timeout") ||
		strings.Contains(err.Error(), "context deadline exceeded")
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
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: string(b)},
		},
	}, nil
}
