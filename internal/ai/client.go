package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// CopilotResponseMsg is sent when the claude CLI returns a plan.
type CopilotResponseMsg struct {
	Thinking string
	Steps    []CopilotStep
	Err      error
}

// Client shells out to the claude CLI for copilot responses.
type Client struct{}

// New returns an error if the claude CLI is not in PATH.
func New(_ string) (*Client, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return nil, fmt.Errorf(
			"AI copilot requires Claude Code CLI\n" +
				"  Install:      claude.ai/code\n" +
				"  Authenticate: claude login",
		)
	}
	return &Client{}, nil
}

// PlanCmd sends the user's intent to the claude CLI and returns a CopilotResponseMsg.
func (c *Client) PlanCmd(intent string, clusterCtx ClusterContext) tea.Cmd {
	return func() tea.Msg {
		steps, err := c.plan(context.Background(), intent, clusterCtx)
		return CopilotResponseMsg{Steps: steps, Err: err}
	}
}

func (c *Client) plan(ctx context.Context, intent string, clusterCtx ClusterContext) ([]CopilotStep, error) {
	prompt := fmt.Sprintf(`%s

Available tools (JSON schema):
%s

User request: %s

Respond with ONLY a JSON array of tool calls and nothing else:
[{"tool": "<name>", "input": {<fields>}}, ...]`,
		BuildSystemPrompt(clusterCtx),
		ToolSchemasJSON(),
		intent,
	)

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := isExitError(err, &exitErr); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("claude cli: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("claude cli: %w", err)
	}

	text := extractResult(out)
	return parseSteps(text)
}

// extractResult pulls the text content out of claude's --output-format json envelope.
// Falls back to the raw output if it doesn't match the expected wrapper shape.
func extractResult(raw []byte) string {
	var wrapper struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Result != "" {
		return wrapper.Result
	}
	return string(raw)
}

// parseSteps extracts a []CopilotStep from a text that should contain a JSON array.
// It handles markdown code fences and leading/trailing prose.
func parseSteps(text string) ([]CopilotStep, error) {
	text = strings.TrimSpace(text)

	// Strip markdown code fences if present
	if idx := strings.Index(text, "```"); idx >= 0 {
		text = text[idx+3:]
		text = strings.TrimPrefix(text, "json")
		text = strings.TrimPrefix(text, "\n")
		if end := strings.Index(text, "```"); end >= 0 {
			text = text[:end]
		}
	}

	// Find the JSON array boundaries
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no tool calls in response: %q", truncate(text, 200))
	}
	text = text[start : end+1]

	var raw []struct {
		Tool  string                 `json:"tool"`
		Input map[string]interface{} `json:"input"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse tool calls: %w", err)
	}

	steps := make([]CopilotStep, 0, len(raw))
	for _, r := range raw {
		steps = append(steps, CopilotStep{
			ToolName:      r.Tool,
			Input:         r.Input,
			IsDestructive: IsDestructive(r.Tool, r.Input),
			Status:        StepPending,
		})
	}
	return steps, nil
}

func isExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
