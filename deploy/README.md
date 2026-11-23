# Deploy Namepiq API on Oracle Always Free

**Target:** 1× Ampere A1 (2 OCPU / 12 GB) Ubuntu aarch64 → Docker Compose (`api` + `postgres`) → Cloudflare Tunnel → `api.namepiq.com`.

UI stays on Cloudflare Pages (`namepiq.com`).

## 1. OCI CLI on your Mac (once)

```bash
brew install oci-cli   # already done if oci --version works
# API key lives in ~/.oci/ — paste the public key in Console → User → API Keys
oci setup config       # or fill ~/.oci/config manually (see agent chat)
oci iam region list --output table
```

## 2. Create the VM (agent can run after CLI auth)

- Shape: `VM.Standard.A1.Flex` · 2 OCPU · 12 GB (Always Free)
- Image: Ubuntu 22.04/24.04 Minimal aarch64
- VCN + public subnet + SSH (22) from your IP; **no** need to open 80/443 if using Tunnel
- Attach SSH public key

## 3. On the VM

```bash
sudo apt update && sudo apt install -y docker.io docker-compose-v2 git
sudo usermod -aG docker ubuntu   # re-login
git clone <namepiq-backend> && cd namepiq-backend
cp deploy/.env.prod.example .env.prod   # fill secrets
docker compose -f deploy/docker-compose.prod.yml --env-file .env.prod up -d --build
curl -sS http://127.0.0.1:8080/healthz
```

## 4. Cloudflare Tunnel

```bash
# On Mac or VM — browser login once
cloudflared tunnel login
cloudflared tunnel create namepiq-api
cloudflared tunnel route dns namepiq-api api.namepiq.com
# copy credentials + deploy/cloudflared.yml.example → /etc/cloudflared/config.yml
sudo cloudflared service install
sudo systemctl enable --now cloudflared
```

Point Pages `NEXT_PUBLIC_API_URL=https://api.namepiq.com`.
OpenSRS allowlist: Tunnel egress IP(s) or VM public IP if you keep one.
