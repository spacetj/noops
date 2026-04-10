# Cloud Run + IAP deployment

This guide packages `noops` as an HTTP MCP server, deploys it to Cloud Run, and locks access down to a single Google account (for example `user@example.com`) through Identity-Aware Proxy (IAP). Update the email and secret names to match your setup.

> **Tip:** Cloud Run validates ID tokens itself when you set `--no-allow-unauthenticated`. That enforcement already relies on Google's Identity-Aware Proxy infrastructure. If you also want the consumer-facing IAP login screen in front of a custom domain, follow the optional load-balancer section.

---

## 1. Prerequisites

- `gcloud` CLI authenticated (`gcloud auth login`) and pointing at the target project: `gcloud config set project <PROJECT_ID>`.
- APIs enabled once per project:
  ```bash
  gcloud services enable \
    run.googleapis.com \
    cloudbuild.googleapis.com \
    artifactregistry.googleapis.com \
    secretmanager.googleapis.com \
    iap.googleapis.com
  ```
- Notion credentials ready (`NOTION_TOKEN`, optional `TASKS_DB_ID`, `GOAL_PAGE_ID`).
- Cloud Run region of choice (examples use `us-central1`).

Set a few helpers for reuse:

```bash
export PROJECT_ID="<your-project>"
export REGION="us-central1"
export SERVICE_NAME="noops-mcp"
export REPO_NAME="noops"
```

---

## 2. Build and publish the container

Create (or reuse) an Artifact Registry repo and push the Cloud Run image.

```bash
# Create the repo once (Docker format)
gcloud artifacts repositories create "$REPO_NAME" \
  --project "$PROJECT_ID" \
  --repository-format=docker \
  --location="$REGION"
# Ignore the "already exists" error on subsequent runs.

IMAGE_TAG="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/noops:$(date +%Y%m%d-%H%M%S)"

# Cloud Build picks up the local Dockerfile and injects the version string
gcloud builds submit . \
  --tag "$IMAGE_TAG" \
  --project "$PROJECT_ID" \
  --machine-type=e2-medium \
  --substitutions _CLI_VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo container)"
```

If you prefer `docker build`, push the image with `docker push "$IMAGE_TAG"`.

---

## 3. Store secrets in Secret Manager

```bash
echo -n "ntn_xxx" | gcloud secrets create notion-token \
  --project "$PROJECT_ID" \
  --replication-policy=automatic \
  --data-file=-

echo -n "<tasks-db-id-or-url>" | gcloud secrets create notion-tasks-db \
  --project "$PROJECT_ID" \
  --replication-policy=automatic \
  --data-file=-

# Optional goal page
echo -n "<goal-page-id-or-url>" | gcloud secrets create notion-goal-page \
  --project "$PROJECT_ID" \
  --replication-policy=automatic \
  --data-file=-
```

Grant Cloud Run's runtime service account permission to read them:

```bash
SERVICE_ACCOUNT="$(gcloud run services describe "$SERVICE_NAME" --region "$REGION" \
  --format 'value(spec.template.spec.serviceAccount)')"
# If service does not exist yet, default is project-number-compute@developer.gserviceaccount.com
if [ -z "$SERVICE_ACCOUNT" ]; then
  PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format 'value(projectNumber)')"
  SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
fi

gcloud secrets add-iam-policy-binding notion-token \
  --project "$PROJECT_ID" \
  --member="serviceAccount:${SERVICE_ACCOUNT}" \
  --role="roles/secretmanager.secretAccessor"

gcloud secrets add-iam-policy-binding notion-tasks-db \
  --project "$PROJECT_ID" \
  --member="serviceAccount:${SERVICE_ACCOUNT}" \
  --role="roles/secretmanager.secretAccessor"

# Optional secret
if gcloud secrets describe notion-goal-page --project "$PROJECT_ID" >/dev/null 2>&1; then
  gcloud secrets add-iam-policy-binding notion-goal-page \
    --project "$PROJECT_ID" \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor"
fi
```

---

## 4. Deploy the Cloud Run service

`noops serve` listens on `$PORT` (default 8080) and checks `ALLOWED_GOOGLE_EMAIL` against the IAP header. Deploy with secrets mounted as environment variables:

```bash
gcloud run deploy "$SERVICE_NAME" \
  --image "$IMAGE_TAG" \
  --region "$REGION" \
  --no-allow-unauthenticated \
  --set-env-vars ALLOWED_GOOGLE_EMAIL="user@example.com" \
  --set-secrets NOTION_TOKEN=notion-token:latest \
  --set-secrets TASKS_DB_ID=notion-tasks-db:latest \
  --set-secrets GOAL_PAGE_ID=notion-goal-page:latest \
  --max-instances 3 \
  --memory 512Mi \
  --cpu 1
```

Confirm the deployed URL—needed later:

```bash
SERVICE_URL="$(gcloud run services describe "$SERVICE_NAME" --region "$REGION" --format 'value(status.url)')"
echo "Cloud Run URL: $SERVICE_URL"
```

---

## 5. Restrict access to your Google account (IAP-style IAM)

Cloud Run checks the caller's identity token against IAM. Remove broad bindings and grant only your Gmail the invoker role:

```bash
# Remove default allUsers binding if it exists
gcloud run services remove-iam-policy-binding "$SERVICE_NAME" \
  --region "$REGION" \
  --member=allUsers \
  --role="roles/run.invoker"

# Allow your Google identity
gcloud run services add-iam-policy-binding "$SERVICE_NAME" \
  --region "$REGION" \
  --member="user:user@example.com" \
  --role="roles/run.invoker"
```

At this point the HTTPS endpoint rejects everyone except the email you granted (and any service accounts you explicitly add). Google issues the same Identity-Aware Proxy ID token that Cloud Run verifies.

---

## 6. (Optional) Front the service with HTTPS Load Balancing + IAP UI

If you need the IAP login screen and/or a custom domain:

1. Reserve a global static IP: `gcloud compute addresses create noops-iap --global`.
2. Point your DNS record at that IP.
3. Create a managed certificate: `gcloud compute ssl-certificates create noops-cert --domains="mcp.example.com"`.
4. Create a serverless NEG: `gcloud compute network-endpoint-groups create noops-neg --region="$REGION" --network-endpoint-type=serverless --cloud-run-service="$SERVICE_NAME"`.
5. Create a backend service and attach the NEG: `gcloud compute backend-services create noops-backend --global --load-balancing-scheme=EXTERNAL --protocol=HTTPS` followed by `gcloud compute backend-services add-backend noops-backend --global --network-endpoint-group=noops-neg --network-endpoint-group-region="$REGION"`.
6. Add a URL map and proxy:
   ```bash
   gcloud compute url-maps create noops-map --default-service=noops-backend
   gcloud compute target-https-proxies create noops-proxy --ssl-certificates=noops-cert --url-map=noops-map
   gcloud compute forwarding-rules create noops-forwarding --global --target-https-proxy=noops-proxy --ports=443 --address=noops-iap
   ```
7. Enable IAP on the backend service: `gcloud iap web enable --resource-type=backend-services --service=noops-backend`.
8. Grant IAP access only to your account: `gcloud iap web add-iam-policy-binding --resource-type=backend-services --service=noops-backend --member="user:user@example.com" --role='roles/iap.httpsResourceAccessor'`.

Use the load balancer hostname instead of the Cloud Run URL in the remaining steps if you enable this option.

---

## 7. Generate IAP identity tokens on demand

Fetch short-lived tokens before each CLI session. Tokens expire after one hour.

```bash
# Authenticate (once per machine)
gcloud auth login
# Application Default Credentials shortcut for gcloud CLIs
gcloud auth application-default login

# Print an identity token with the Cloud Run URL as the audience
export NOOPS_IAP_AUDIENCE="$SERVICE_URL"
export NOOPS_IAP_TOKEN="$(gcloud auth print-identity-token --audiences="$NOOPS_IAP_AUDIENCE")"
```

For the optional HTTPS load balancer, set `NOOPS_IAP_AUDIENCE` to the IAP OAuth client ID shown in the Cloud Console (Target is the backend service).

> **Automation idea:** wrap the two export commands in a helper script (`script.sh`) and source it before launching Codex CLI to keep the token fresh.

---

## 8. Wire the remote MCP into Codex CLI

1. Locate the Codex CLI config file (default `~/.config/codex/config.json`).
2. Add or update a server entry to point at the Cloud Run endpoint:

```json
{
  "servers": {
    "noops-cloudrun": {
      "type": "mcp",
      "transport": {
        "type": "http",
        "url": "${SERVICE_URL}/mcp",
        "headers": {
          "Authorization": "Bearer ${NOOPS_IAP_TOKEN}"
        }
      }
    }
  }
}
```

3. Export `NOOPS_IAP_TOKEN` in the shell **before** launching Codex CLI (tokens are not automatically refreshed).
4. Start Codex CLI and select the `noops-cloudrun` MCP server.
   - The CLI should receive the `initialize`, `tools/list`, and `tools/call` responses via HTTPS.

If Codex CLI supports command-based header injection, replace the static header with a helper script that prints the token dynamically.

---

## 9. Smoke test the deployment

```bash
# Verify the health endpoint (authorized token required)
curl -H "Authorization: Bearer ${NOOPS_IAP_TOKEN}" "$SERVICE_URL/healthz"

# List available tools via JSON-RPC
test_payload='{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
curl -s \
  -H "Authorization: Bearer ${NOOPS_IAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$test_payload" \
  "$SERVICE_URL/mcp" | jq
```

You should see the known MCP tools (`task.create`, `task.update`, etc.). Failed calls usually point to missing secrets or expired identity tokens.

---

## 10. Maintenance checklist

- Refresh the container (`gcloud builds submit …`) whenever the CLI code changes.
- Rotate secrets by updating Secret Manager versions—Cloud Run picks up new versions on the next revision deployment.
- Re-run the token helper before each Codex session, or automate it in your shell profile.
- Keep the IAM bindings tight: only the intended Google identity (for example `user@example.com`) and required service accounts should have `roles/run.invoker` or IAP access.
