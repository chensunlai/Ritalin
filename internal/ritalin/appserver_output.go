package ritalin

import (
	"fmt"
	"strings"
)

// Show tool lifecycle and output, not the complete patch as a wall of HTML.
func displayTool(item appItem, completed bool, streamed map[string]string, display func(string) error) error {
	if item.Type == "" || item.Type == "agentMessage" || item.Type == "userMessage" || item.Type == "reasoning" || item.Type == "plan" {
		return nil
	}
	status := item.Status
	if status == "" {
		status = "started"
		if completed {
			status = "completed"
		}
	}
	detail := ""
	switch item.Type {
	case "commandExecution":
		if completed {
			previous := streamed[item.ID]
			if strings.HasPrefix(item.AggregatedOutput, previous) {
				if tail := strings.TrimPrefix(item.AggregatedOutput, previous); tail != "" {
					if err := display(tail); err != nil {
						return err
					}
				}
			}
			if item.ExitCode != nil {
				detail = fmt.Sprintf("exit %d", *item.ExitCode)
			}
			delete(streamed, item.ID)
		} else {
			detail = item.Command
		}
	case "fileChange":
		var paths []string
		for _, change := range item.Changes {
			paths = append(paths, change.Path)
		}
		detail = strings.Join(paths, ", ")
	case "mcpToolCall":
		detail = item.Server + "/" + item.Tool
	case "webSearch":
		detail = item.Query
	default:
		detail = item.Tool
	}
	return display(fmt.Sprintf("\n[%s · %s] %s\n", item.Type, status, detail))
}
