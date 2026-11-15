package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/store"
)

// DefaultWait and DefaultMaxWait are review_wait's timeout defaults.
const (
	DefaultWait    = 300 * time.Second
	DefaultMaxWait = 900 * time.Second
)

// keepAliveEvery is how often review_wait sends a progress notification
// while blocked (only when the client asked for progress).
var keepAliveEvery = 20 * time.Second

type reviewWaitIn struct {
	Task     string `json:"task,omitempty" jsonschema:"Task id to wait on, e.g. T-12; omit to wait for a round on any task."`
	Doc      string `json:"doc,omitempty" jsonschema:"Document slug within task (e.g. plan); omit for any document of the task."`
	TimeoutS int    `json:"timeout_s,omitempty" jsonschema:"Seconds to wait. Default 300; the server caps it (900 unless configured lower). Keep it below your client's MCP tool timeout."`
}

const reviewWaitDesc = `Wait until the owner submits a review round, then return the pending rounds in the same form as review_pending. Give doc and task to wait for one document, only task for any document of that task, or nothing for any task. If matching rounds are already pending, it returns at once. On timeout it returns {"waiting": true}: call it again, or do other work and check review_pending later. Use it after you send the owner a review link, so they do not have to come back to the chat. The owner can interrupt you from the chat at any time.

Client timeouts: Codex stops an MCP call after 60 s unless its server entry sets tool_timeout_sec (e.g. 960). Claude Code stops an HTTP tool call that receives nothing for 5 minutes unless its server entry sets "timeout" in ms (e.g. 960000). The server may also cap the wait (90 s behind a proxy). Keep timeout_s below your client's limit.`

func (s *Server) registerWait(srv *mcp.Server) {
	maxWait := s.MaxWait
	if maxWait <= 0 {
		maxWait = DefaultMaxWait
	}
	tool := &mcp.Tool{
		Name:        "review_wait",
		Title:       "Wait for review",
		Description: reviewWaitDesc,
		InputSchema: schema[reviewWaitIn](nil),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
	mcp.AddTool(srv, tool, func(ctx context.Context, req *mcp.CallToolRequest, in reviewWaitIn) (*mcp.CallToolResult, any, error) {
		timeout := DefaultWait
		if in.TimeoutS > 0 {
			timeout = time.Duration(in.TimeoutS) * time.Second
		}
		timeout = min(timeout, maxWait)
		p := store.WaitParams{Task: in.Task, Slug: in.Doc, Timeout: timeout}
		if req != nil && req.Params != nil && req.Session != nil {
			if token := req.Params.GetProgressToken(); token != nil {
				p.TickEvery = keepAliveEvery
				p.Tick = func(elapsed time.Duration) {
					_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
						ProgressToken: token,
						Progress:      elapsed.Seconds(),
						Total:         timeout.Seconds(),
						Message:       fmt.Sprintf("waiting for review, %ds of %ds", int(elapsed.Seconds()), int(timeout.Seconds())),
					})
				}
			}
		}
		start := time.Now()
		rs, err := s.Store.WaitRounds(ctx, p)
		if err != nil {
			return nil, nil, err
		}
		if len(rs) == 0 {
			return nil, map[string]any{"waiting": true, "waited_s": int(time.Since(start).Seconds()),
				"hint": "no round submitted yet; call review_wait again or check review_pending later"}, nil
		}
		out := make([]roundOut, len(rs))
		for i, r := range rs {
			out[i] = s.round(r)
		}
		return nil, map[string]any{"rounds": out}, nil
	})
}
