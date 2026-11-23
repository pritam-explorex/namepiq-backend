# Stage E2E (API)

Real-network suite against OpenSRS horizon + Razorpay **test** keys.

## Requirements

- Postgres (`DATABASE_URL`)
- `OPENSRS_USERNAME`, `OPENSRS_API_KEY`, `OPENSRS_ENDPOINT`
- `RAZORPAY_KEY_ID` / `RAZORPAY_KEY_SECRET` (`rzp_test_*` only)
- Optional: `RAZORPAY_WEBHOOK_SECRET` for refund/dispute webhook tests
- Loads `.env` / `.env.stage` from repo root; forces `E2E_MAIL_CAPTURE=1`

## Run

```bash
cd namepiq-backend
go test -tags=stage -count=1 -timeout 20m ./test/stage/...
```

Without OpenSRS/Razorpay env, `TestMain` exits 0 (skip). Live Razorpay keys are refused.

## Mail capture (for Playwright)

Start the API with:

```bash
E2E_MAIL_CAPTURE=1 ./bin/api
```

Then:

- `GET /v1/test/last-email?to=user@…` → `{ verify_token, verify_path, … }`
- Disabled / hard-error if `APP_ENV=production`
