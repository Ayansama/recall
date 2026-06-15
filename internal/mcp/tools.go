package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"recall/internal/storage"
)

const (
	defaultSessionLimit  = 10
	defaultTokenBudget   = 4000
	toolListSessions     = "list_sessions"
	toolGetSessionContext = "get_session_context"
	toolGetLatestError   = "get_latest_error"
)

// listSessionsArgs decodes list_sessions tool parameters.
type listSessionsArgs struct {
	Limit int `json:"limit"`
}

// getSessionContextArgs decodes get_session_context tool parameters.
type getSessionContextArgs struct {
	SessionID   string `json:"session_id"`
	TokenBudget int    `json:"token_budget"`
}

// getLatestErrorArgs decodes get_latest_error tool parameters.
type getLatestErrorArgs struct {
	SessionID string `json:"session_id"`
}

// registerTools attaches Recall MCP tool definitions and handlers to the server.
func registerTools(s *server.MCPServer, db *storage.DB) {
	s.AddTool(
		mcpsdk.NewTool(toolListSessions,
			mcpsdk.WithDescription("Browse the catalog of captured terminal sessions"),
			mcpsdk.WithInteger("limit",
				mcpsdk.Description("Maximum number of sessions to return"),
				mcpsdk.DefaultNumber(defaultSessionLimit),
			),
		),
		mcpsdk.NewTypedToolHandler(func(ctx context.Context, req mcpsdk.CallToolRequest, args listSessionsArgs) (*mcpsdk.CallToolResult, error) {
			return handleListSessions(db, args)
		}),
	)

	s.AddTool(
		mcpsdk.NewTool(toolGetSessionContext,
			mcpsdk.WithDescription("Retrieve a sliced history block for a terminal session"),
			mcpsdk.WithString("session_id",
				mcpsdk.Required(),
				mcpsdk.Description("Target session identifier"),
			),
			mcpsdk.WithInteger("token_budget",
				mcpsdk.Description("Approximate token budget for returned context"),
				mcpsdk.DefaultNumber(defaultTokenBudget),
			),
		),
		mcpsdk.NewTypedToolHandler(func(ctx context.Context, req mcpsdk.CallToolRequest, args getSessionContextArgs) (*mcpsdk.CallToolResult, error) {
			return handleGetSessionContext(db, args)
		}),
	)

	s.AddTool(
		mcpsdk.NewTool(toolGetLatestError,
			mcpsdk.WithDescription("Pull the latest crash context from terminal logs"),
			mcpsdk.WithString("session_id",
				mcpsdk.Description("Optional session scope; searches all sessions when omitted"),
			),
		),
		mcpsdk.NewTypedToolHandler(func(ctx context.Context, req mcpsdk.CallToolRequest, args getLatestErrorArgs) (*mcpsdk.CallToolResult, error) {
			return handleGetLatestError(db, args)
		}),
	)
}

func handleListSessions(db *storage.DB, args listSessionsArgs) (*mcpsdk.CallToolResult, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = defaultSessionLimit
	}

	sessions, err := db.ListSessions(limit)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list sessions: %v", err)), nil
	}

	payload, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("encode sessions: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(payload)), nil
}

func handleGetSessionContext(db *storage.DB, args getSessionContextArgs) (*mcpsdk.CallToolResult, error) {
	if args.SessionID == "" {
		return mcpsdk.NewToolResultError("session_id is required"), nil
	}

	budget := args.TokenBudget
	if budget <= 0 {
		budget = defaultTokenBudget
	}

	charBudget := budget * 4

	head, tail, totalCount, err := db.GetSessionContextLines(args.SessionID)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get session lines: %v", err)), nil
	}

	if totalCount == 0 {
		emptyMsg := wrapOutput(args.SessionID, "")
		return mcpsdk.NewToolResultText(emptyMsg), nil
	}

	delimiter := "\n\n[... TEXT TRUNCATED BY RECALL ENGINE ...]\n\n"
	hasDelimiter := totalCount > 175

	var finalContext string
	
	// Helper to join lines
	joinLines := func(lines []string) string {
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n")
	}

	// Shrinking loop
	for {
		headText := joinLines(head)
		tailText := joinLines(tail)

		neededLen := len(headText) + len(tailText)
		if hasDelimiter {
			neededLen += len(delimiter)
		}

		if neededLen <= charBudget {
			if hasDelimiter {
				if len(head) == 0 && len(tail) == 0 {
					finalContext = ""
				} else if len(head) > 0 && len(tail) > 0 {
					finalContext = headText + delimiter + tailText
				} else if len(head) > 0 {
					finalContext = headText + delimiter
				} else {
					finalContext = delimiter + tailText
				}
			} else {
				finalContext = headText
			}
			break
		}

		// Shrink
		if len(tail) > 0 {
			tail = tail[1:] // remove oldest tail line
		} else if len(head) > 0 {
			head = head[:len(head)-1] // remove newest head line
		} else {
			// Even with empty head and tail, it doesn't fit? This means charBudget < len(delimiter)
			if charBudget >= len(delimiter) {
				finalContext = delimiter
			} else if charBudget > 0 {
				finalContext = delimiter[:charBudget]
			} else {
				finalContext = ""
			}
			break
		}
	}

	wrapped := wrapOutput(args.SessionID, finalContext)
	return mcpsdk.NewToolResultText(wrapped), nil
}

func handleGetLatestError(db *storage.DB, args getLatestErrorArgs) (*mcpsdk.CallToolResult, error) {
	lines, actualSessID, err := db.GetLatestErrorWindow(args.SessionID)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get latest error: %v", err)), nil
	}

	payload, err := json.MarshalIndent(lines, "", "  ")
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("encode error window: %v", err)), nil
	}

	sessID := actualSessID
	if sessID == "" {
		sessID = args.SessionID
	}
	if sessID == "" {
		sessID = "UNKNOWN"
	}

	wrapped := wrapOutput(sessID, string(payload))
	return mcpsdk.NewToolResultText(wrapped), nil
}

func wrapOutput(sessionID string, content string) string {
	return fmt.Sprintf("--- START RECALL LOG BLOCK [SESSION: %s] ---\n%s\n--- END RECALL LOG BLOCK ---", sessionID, content)
}

// ParseListSessionsLimit extracts the limit parameter using CallToolRequest helpers.
func ParseListSessionsLimit(req mcpsdk.CallToolRequest) int {
	return req.GetInt("limit", defaultSessionLimit)
}

// ParseGetSessionContextParams extracts get_session_context parameters.
func ParseGetSessionContextParams(req mcpsdk.CallToolRequest) (sessionID string, tokenBudget int) {
	return req.GetString("session_id", ""), req.GetInt("token_budget", defaultTokenBudget)
}

// ParseGetLatestErrorSessionID extracts the optional session_id parameter.
func ParseGetLatestErrorSessionID(req mcpsdk.CallToolRequest) string {
	return req.GetString("session_id", "")
}
