// Smoke-test the Namepiq MCP server over stdio (list tools + search_domains).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	bin := os.Getenv("MCP_BIN")
	if bin == "" {
		bin = "./bin/mcp"
	}
	base := os.Getenv("NAMEPIQ_API_BASE")
	if base == "" {
		base = "http://localhost:8080"
	}

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), "NAMEPIQ_API_BASE="+base)
	cmd.Dir = findRepoRoot()

	client := mcp.NewClient(&mcp.Implementation{Name: "namepiq-smoke", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("tools (%d):\n", len(tools.Tools))
	for _, t := range tools.Tools {
		fmt.Printf("  - %s\n", t.Name)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_domains",
		Arguments: map[string]any{"query": "namepiq"},
	})
	if err != nil {
		log.Fatalf("search_domains: %v", err)
	}
	if res.IsError {
		log.Fatalf("search_domains error: %v", res.Content)
	}
	fmt.Println("\nsearch_domains ok:")
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			fmt.Println(tc.Text)
		}
	}
}

func findRepoRoot() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
