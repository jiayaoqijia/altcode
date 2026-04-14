# AltFix Deployment

Three methods to deploy the AltFix coding agent daemon.

## 1. Docker

Build and run the container image:

```bash
make build                    # produces dist/altcode
docker build -f deployments/Dockerfile -t altfix .
docker run -d -p 9100:9100 \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  -e OPENAI_API_KEY=sk-... \
  altfix
```

## 2. Setup Script (bare-metal Ubuntu 24.04)

One-line install on a fresh VM:

```bash
curl -fsSL https://altcode.io/setup-vm.sh | sudo bash
```

Or manually:

```bash
sudo bash scripts/setup-vm.sh
# Configure /home/altfix/.altcode/env with API keys
sudo systemctl start altfix
```

## 3. GCP VM Image

Bake a reusable image with Packer or from a configured VM:

```bash
# 1. Create a VM and run the setup script
# 2. Stop the VM
# 3. Create an image from the disk
gcloud compute images create altfix-v1 --source-disk=altfix-vm --source-disk-zone=us-central1-a
```

## Health Check

```bash
curl http://localhost:9100/health
```

## Configuration

API keys and auth tokens go in `/home/altfix/.altcode/env` (systemd `EnvironmentFile`).
