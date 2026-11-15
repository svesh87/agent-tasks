package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The owner's approval reaches the agent: review_wait on the document returns
// it (and keeps returning it until a new revision), doc_get, task_get and
// review_pending show it.
func TestApprovalOverMCP(t *testing.T) {
	st, cs := waitEnv(t, 0, nil)
	ctx := context.Background()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return structured(t, res)
	}

	if got := call("doc_get", map[string]any{"task": "T-1", "slug": "plan"}); got["approved"] != nil {
		t.Fatalf("doc_get before approval: %v", got["approved"])
	}

	done := make(chan map[string]any, 1)
	go func() {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"task": "T-1", "doc": "plan", "timeout_s": 10}})
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- structured(t, res)
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := st.Approve(ctx, "T-1", "plan", 1, false); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		a, _ := got["approved"].(map[string]any)
		if a == nil || a["revision"] != float64(1) || a["by"] != "owner" || got["url"] != "https://tasks.example/t/T-1/d/plan/r/1" {
			t.Fatalf("review_wait: %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review_wait did not return on approval")
	}

	// Not consumed: waiting again returns at once.
	if got := call("review_wait", map[string]any{"task": "T-1", "doc": "plan", "timeout_s": 5}); got["approved"] == nil {
		t.Fatalf("second review_wait: %v", got)
	}
	if got := call("doc_get", map[string]any{"task": "T-1", "slug": "plan"}); got["approved"] == nil {
		t.Fatalf("doc_get after approval: %v", got)
	}
	tg := call("task_get", map[string]any{"id": "T-1"})
	docs, _ := tg["documents"].([]any)
	if len(docs) != 1 || docs[0].(map[string]any)["approved"] == nil {
		t.Fatalf("task_get documents: %v", tg["documents"])
	}
	rp := call("review_pending", map[string]any{"task": "T-1"})
	if as, _ := rp["approvals"].([]any); len(as) != 1 {
		t.Fatalf("review_pending approvals: %v", rp["approvals"])
	}

	// A new revision removes the approval: review_wait waits again.
	call("doc_write", map[string]any{"task": "T-1", "slug": "plan", "body": "y\n"})
	if got := call("review_wait", map[string]any{"task": "T-1", "doc": "plan", "timeout_s": 1}); got["waiting"] != true {
		t.Fatalf("review_wait after a new revision: %v", got)
	}
}
