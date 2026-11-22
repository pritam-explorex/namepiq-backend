# Namepiq Backend

Go API for Namepiq — auth, OpenSRS domains, Razorpay, Postgres.

Runs on **Oracle Always Free**. Website: [namepiq-ui](https://github.com/pritamiitkgp/namepiq-ui).

## Dev

```bash
cp .env.example .env
docker compose up -d postgres   # from this repo
go run ./cmd/api
```

Health: `GET http://localhost:8080/healthz`
