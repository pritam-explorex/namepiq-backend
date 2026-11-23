# Namepiq Backend

Go API + MCP for Namepiq — auth, OpenSRS domains, Razorpay, Postgres.

Runs on **Oracle Always Free**. Website: [namepiq-ui](https://github.com/pritamiitkgp/namepiq-ui).

## Dev

```bash
cp .env.example .env
docker compose up -d postgres   # from this repo
go run ./cmd/api
```

Health: `GET http://localhost:8080/healthz`

## MCP (Cursor / Claude Desktop)

Stdio server that wraps the HTTP API. Start the API first, then add to Cursor MCP settings:

```json
{
  "mcpServers": {
    "namepiq": {
      "command": "go",
      "args": ["run", "./cmd/mcp"],
      "cwd": "/absolute/path/to/namepiq-backend",
      "env": {
        "NAMEPIQ_API_BASE": "http://localhost:8080",
        "NAMEPIQ_TOKEN": ""
      }
    }
  }
}
```

Or `make build-mcp` and point `command` at `bin/mcp`.

**Tools:** `search_domains`, `suggest_domains`, `quote_domain`, `login`, `get_me`, `upsert_contact`, `list_domains`, `get_domain`, `register_domain`, `renew_domain`, `update_domain`, `push_domain_contacts`, `get_nameservers`, `set_nameservers`, `get_dns`, `set_dns`.

Public search/suggest/quote need no auth. Everything else: set `NAMEPIQ_TOKEN` (JWT from login) or call the `login` tool once per session.

## Auth

- `POST /v1/auth/register` — creates user, sends Resend verify email
- `POST /v1/auth/login`
- `GET|POST /v1/auth/verify-email?token=`
- `POST /v1/auth/resend-verification` (auth required)
- `GET /v1/me` / `PUT /v1/me/contact` (auth required)

```env
RESEND_API_KEY=re_...
RESEND_FROM_EMAIL=Namepiq <noreply@namepiq.com>
APP_BASE_URL=http://localhost:3000
```

Without `RESEND_API_KEY`, the verify link is logged by the API (`[mail:dev]`).

## Domain search (OpenSRS)

```bash
curl -s 'localhost:8080/v1/domains/search?q=example'
curl -s 'localhost:8080/v1/domains/search?q=example.com'
```

Set `OPENSRS_USERNAME` + `OPENSRS_API_KEY` (horizon endpoint by default). Retail INR uses `USD_INR_RATE` and `RETAIL_MARKUP_PERCENT`.

## Tests

```bash
go test ./...
# or
make test
```

Coverage includes auth, Resend, OpenSRS client (mocked), domain search service, and HTTP flows.
