package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/namepiq/api/internal/mcpserver"
)

func main() {
	_ = godotenv.Load()

	base := os.Getenv("NAMEPIQ_API_BASE")
	if base == "" {
		base = os.Getenv("API_BASE_URL")
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	token := os.Getenv("NAMEPIQ_TOKEN")

	api := mcpserver.NewClient(base, token)
	server := mcpserver.New(api)

	// MCP over stdio — Cursor/Claude Desktop spawn this process.
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mcp: %v", err)
	}
}
