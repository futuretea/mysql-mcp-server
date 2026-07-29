package mcp

import (
	"context"
	"errors"
	"testing"

	mcpprotocol "github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/mysql-mcp-server/pkg/toolset"
)

func TestNewToolHandlerReturnsErrorResult(t *testing.T) {
	wantErr := errors.New("boom")
	handler := newToolHandler(toolset.ServerTool{
		Handler: func(_ context.Context, params map[string]any) (string, error) {
			if params == nil {
				t.Fatal("handler params must be a non-nil map")
			}
			return "", wantErr
		},
	})

	result, err := handler(t.Context(), mcpprotocol.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler transport error = %v, want nil", err)
	}
	if !result.IsError {
		t.Fatal("handler result IsError = false, want true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("handler content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(mcpprotocol.TextContent)
	if !ok {
		t.Fatalf("handler content type = %T, want mcp.TextContent", result.Content[0])
	}
	if text.Type != "text" || text.Text != wantErr.Error() {
		t.Fatalf("handler text = {Type:%q Text:%q}, want {Type:%q Text:%q}", text.Type, text.Text, "text", wantErr)
	}
}

func TestNewToolHandlerRejectsNonObjectMySQLArguments(t *testing.T) {
	calls := 0
	handler := newToolHandler(toolset.ServerTool{
		Domain: "mysql",
		Handler: func(context.Context, map[string]any) (string, error) {
			calls++
			return "ok", nil
		},
	})

	for _, arguments := range []any{[]any{"unexpected"}, "unexpected"} {
		request := mcpprotocol.CallToolRequest{Params: mcpprotocol.CallToolParams{Arguments: arguments}}
		result, err := handler(t.Context(), request)
		if err != nil {
			t.Fatalf("handler transport error = %v, want nil", err)
		}
		if !result.IsError || len(result.Content) != 1 {
			t.Fatalf("invalid arguments result = %+v, want one error result", result)
		}
		text, ok := result.Content[0].(mcpprotocol.TextContent)
		if !ok || text.Text != "[mysql_invalid_params] invalid parameters" {
			t.Fatalf("invalid arguments text = %#v, want mysql_invalid_params", result.Content[0])
		}
	}
	if calls != 0 {
		t.Fatalf("handler calls = %d, want 0", calls)
	}

	result, err := handler(t.Context(), mcpprotocol.CallToolRequest{Params: mcpprotocol.CallToolParams{Arguments: map[string]any{}}})
	if err != nil || result.IsError || calls != 1 {
		t.Fatalf("empty object result = %+v, %v, calls=%d; want success and one call", result, err, calls)
	}
}
