package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New builds the Namepiq MCP server (tools wrap the HTTP API).
func New(api *Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "namepiq",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_domains",
		Description: "Check domain availability and retail prices for a query or FQDN. Optional ISO currency (INR,USD,EUR,...).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Query    string `json:"query" jsonschema:"domain keyword or FQDN to search"`
		Currency string `json:"currency,omitempty" jsonschema:"ISO currency e.g. USD EUR INR"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.Search(ctx, in.Query, in.Currency))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "suggest_domains",
		Description: "AI/OpenSRS name suggestions for a brand or keyword. Optional comma-separated tlds (e.g. com,io,ai).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Query string `json:"query" jsonschema:"brand or keyword"`
		TLDs  string `json:"tlds,omitempty" jsonschema:"optional comma-separated TLDs"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.Suggest(ctx, in.Query, in.TLDs))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "quote_domain",
		Description: "Get a multi-year retail quote for an exact domain (1,2,3,5 years). Optional ISO currency.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Domain   string `json:"domain" jsonschema:"FQDN e.g. example.com"`
		Years    int    `json:"years,omitempty" jsonschema:"registration years; default 1"`
		Currency string `json:"currency,omitempty" jsonschema:"ISO currency e.g. USD EUR INR"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.Quote(ctx, in.Domain, in.Years, in.Currency))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "login",
		Description: "Authenticate with Namepiq email/password. Stores JWT for subsequent authenticated tools in this session.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Email    string `json:"email" jsonschema:"account email"`
		Password string `json:"password" jsonschema:"account password"`
	}) (*mcp.CallToolResult, any, error) {
		raw, err := api.Login(ctx, in.Email, in.Password)
		if err != nil {
			return toolErr(err), nil, nil
		}
		// Don't echo password; strip token from text if we want safer display — still return JSON for agent use.
		return textResult(string(raw)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_me",
		Description: "Return the authenticated user profile and default contact (requires login or NAMEPIQ_TOKEN).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.Me(ctx))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "upsert_contact",
		Description: "Create/update the account's default registrant contact used for registrations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.UpsertContact(ctx, in))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_domains",
		Description: "List domains owned by the authenticated account.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.ListDomains(ctx))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_domain",
		Description: "Get one owned domain by UUID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"domain UUID from list_domains"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.GetDomain(ctx, in.ID))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "register_domain",
		Description: "Register an available domain via OpenSRS (charges reseller account). Requires verified contact on file. Prefer quote_domain first. Payment/checkout not wired yet.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Domain    string `json:"domain" jsonschema:"FQDN to register"`
		Years     int    `json:"years,omitempty" jsonschema:"1,2,3,or 5; default 1"`
		AutoRenew bool   `json:"auto_renew,omitempty" jsonschema:"enable auto-renew"`
	}) (*mcp.CallToolResult, any, error) {
		years := in.Years
		if years == 0 {
			years = 1
		}
		return jsonTool(api.RegisterDomain(ctx, in.Domain, years, in.AutoRenew))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "renew_domain",
		Description: "Renew an owned domain for additional years.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID    string `json:"id" jsonschema:"domain UUID"`
		Years int    `json:"years,omitempty" jsonschema:"years to add; default 1"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.RenewDomain(ctx, in.ID, in.Years))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_domain",
		Description: "Update lock and/or auto-renew flags on an owned domain.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID        string `json:"id" jsonschema:"domain UUID"`
		Locked    *bool  `json:"locked,omitempty" jsonschema:"registrar lock"`
		AutoRenew *bool  `json:"auto_renew,omitempty" jsonschema:"auto-renew flag"`
	}) (*mcp.CallToolResult, any, error) {
		body := map[string]any{}
		if in.Locked != nil {
			body["locked"] = *in.Locked
		}
		if in.AutoRenew != nil {
			body["auto_renew"] = *in.AutoRenew
		}
		if len(body) == 0 {
			return toolErr(fmt.Errorf("provide locked and/or auto_renew")), nil, nil
		}
		return jsonTool(api.PatchDomain(ctx, in.ID, body))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "push_domain_contacts",
		Description: "Push the account default contact to OpenSRS for this domain (owner/admin/billing/tech).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"domain UUID"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.PushContacts(ctx, in.ID))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_nameservers",
		Description: "Read nameservers for an owned domain.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"domain UUID"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.GetNameservers(ctx, in.ID))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_nameservers",
		Description: "Replace nameservers for an owned domain (unlock may be required).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID          string   `json:"id" jsonschema:"domain UUID"`
		Nameservers []string `json:"nameservers" jsonschema:"list of NS hostnames"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.SetNameservers(ctx, in.ID, in.Nameservers))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_dns",
		Description: "Get OpenSRS Managed DNS records for an owned domain.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"domain UUID"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.GetDNS(ctx, in.ID))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_dns",
		Description: "Replace OpenSRS Managed DNS zone records for an owned domain. Pass records as returned by get_dns (array of record objects).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID      string `json:"id" jsonschema:"domain UUID"`
		Records any    `json:"records" jsonschema:"DNS record list"`
	}) (*mcp.CallToolResult, any, error) {
		return jsonTool(api.SetDNS(ctx, in.ID, in.Records))
	})

	return s
}

func jsonTool(raw json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return toolErr(err), nil, nil
	}
	pretty, pErr := json.MarshalIndent(json.RawMessage(raw), "", "  ")
	if pErr != nil {
		return textResult(string(raw)), nil, nil
	}
	return textResult(string(pretty)), nil, nil
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}
}

func toolErr(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
