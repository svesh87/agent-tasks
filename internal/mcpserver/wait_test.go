package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/links"
	"github.com/svesh87/agent-tasks/internal/store"
)

func waitEnv(t *testing.T, maxWait time.Duration, progress *atomic.Int32) (*store.Store, *mcp.ClientSession) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := (&Server{Store: st, Links: links.New("https://tasks.example"), WorkspaceRoot: "~/agent-work", MaxWait: maxWait}).New()
	ct, stt := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, stt, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	opts := &mcp.ClientOptions{ProgressNotificationHandler: func(context.Context, *mcp.ProgressNotificationClientRequest) {
		if progress != nil {
			progress.Add(1)
		}
	}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, opts).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	task, _ := st.CreateTask(ctx, store.NewTask{Title: "t"})
	st.WriteDoc(ctx, store.WriteDocParams{Task: task.ID, Slug: "plan", Body: "x\n", Author: store.AuthorAgent})
	return st, cs
}

func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", text(res))
	}
	var out map[string]any
	b, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(b, &out)
	return out
}

func TestReviewWaitSubmit(t *testing.T) {
	var progress atomic.Int32
	old := keepAliveEvery
	keepAliveEvery = 50 * time.Millisecond
	t.Cleanup(func() { keepAliveEvery = old })
	st, cs := waitEnv(t, 0, &progress)
	ctx := context.Background()

	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		params := &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"task": "T-1", "doc": "plan", "timeout_s": 10}}
		params.SetProgressToken("w1")
		res, err := cs.CallTool(ctx, params)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	time.Sleep(300 * time.Millisecond)
	r, _ := st.DraftRound(ctx, "T-1", "plan", 0)
	st.AddComment(ctx, r.ID, store.NewComment{Body: "please fix"})
	if _, err := st.SubmitRound(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		out := structured(t, res)
		rounds, _ := out["rounds"].([]any)
		if len(rounds) != 1 || rounds[0].(map[string]any)["url"] != "https://tasks.example/t/T-1/d/plan/r/1" {
			t.Fatalf("review_wait %v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("review_wait not woken")
	}
	if progress.Load() < 2 {
		t.Errorf("progress notifications: %d", progress.Load())
	}
}

func TestReviewWaitTimeoutCapped(t *testing.T) {
	_, cs := waitEnv(t, 1*time.Second, nil)
	start := time.Now()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"timeout_s": 600}})
	if err != nil {
		t.Fatal(err)
	}
	out := structured(t, res)
	if out["waiting"] != true {
		t.Fatalf("timeout result %v", out)
	}
	if el := time.Since(start); el < 900*time.Millisecond || el > 3*time.Second {
		t.Fatalf("cap not applied: %v", el)
	}
}

func TestReviewWaitCancel(t *testing.T) {
	_, cs := waitEnv(t, 0, nil)
	c, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := cs.CallTool(c, &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"timeout_s": 60}})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("cancel took %v", el)
	}
}
