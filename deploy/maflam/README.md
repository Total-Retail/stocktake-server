# Maflam Trading — deployment (Vantage, Linux cloud)

Maflam Trading runs the StockCount / Vantage Suite server on one Linux cloud box, against
**Vantage Retail on Business Central online** (`ERP_BACKEND=vantage`). Testing and live run
side by side on that box as separate Docker Compose projects. Yellowcob/SPAR
is separate (LS Central, VPN, Windows/WSL: `.github/workflows/spar_production.yml`).

| | `maflam-testing` | `maflam-live` |
|---|---|---|
| Deploys | every push to `main`, or **Run workflow → testing** | **Run workflow → live** only |
| Branch | `main` only | `main` only |
| Approval | — | required approver if the GitHub plan allows it (Enterprise for private repos) |
| Compose project | `maflam-testing` | `maflam-live` |
| Directory on the box (APP_DIR) | `/home/dev/stocktake/testing` | `/home/dev/stocktake/live` |
| Domain | maflam.stockcount-test.totalretailzw.com | maflam.stockcount.totalretailzw.com |
| Port, only with nginx on the host (127.0.0.1) | 8085 | 8084 |
| BC environment/company, database, secrets | its own | its own |

Workflow: `.github/workflows/maflam_deploy.yml`. It runs the Go tests, writes the stack's
`.env` from the environment's variables and secrets, copies the code to `APP_DIR` over SSH,
and starts the stack. It derives `docker-compose.stack.yml` from `docker-compose.yml` on each
deploy: the fixed container names are removed (SPAR's deploy keeps them, so the shared file
is unchanged) and the stack's nginx either joins the box's nginx network (`PROXY_NETWORK`, with
`VIRTUAL_HOST`, in `docker-compose.edge.yml`) or listens on `127.0.0.1:HTTP_PORT`. Then it runs
`docker compose -p maflam-<target> -f docker-compose.stack.yml [-f docker-compose.edge.yml] up --build -d`, waits for the
API to report healthy, and checks `/api/v1/system/info` says `erp_backend: vantage` with
`label_worksheets`. Each project has its own containers, network, volumes (database, cache)
and images, so one stack's deploy never touches the other's data.

## One-time setup

1. **Deploy key**: the box (72.60.20.70) already has the user `dev`, which logs in with the
   key `C:\Users\Wyne\.ssh\devtr`. Both environments use it (`SSH_USER=dev`,
   `SSH_PRIVATE_KEY_FILE=~/.ssh/devtr`). For another box, make a key with `ssh-keygen -t ed25519`
   and pass its .pub to the bootstrap with `--key`.
2. **The box** (72.60.20.70): `setup-github.ps1` checks it over SSH as `dev` (Docker without
   sudo, rsync) and creates each environment's `APP_DIR` under `/home/dev/stocktake` - no root
   needed there. `server-bootstrap.sh --user dev` (as root) is only for a box that lacks Docker or
   rsync, or for folders outside the user's home.
   The stacks sit **behind the nginx already on the box**, as 3retail-licensing does
   (`EDGE_MODE=proxy`, `scripts/server/server.sh` there):
   - **nginx in docker** (nginx-proxy): `PROXY_NETWORK` is its docker network. The stack's
     nginx joins it with `VIRTUAL_HOST=<domain>` and publishes no port; nginx-proxy routes the
     domain by itself. With acme-companion on the box the deploy also sets `LETSENCRYPT_HOST`
     (certificate on the box); without it, `HTTPS_METHOD=nohttps` (TLS elsewhere, e.g. Cloudflare).
     `setup-github.ps1` looks at the box over SSH and fills `PROXY_NETWORK` in when it is empty.
   - **nginx on the host**: leave `PROXY_NETWORK` empty; the stack listens on 127.0.0.1:8085
     (testing) / 127.0.0.1:8084 (live). Add a server block per domain that proxies there, with
     `proxy_set_header Host $host; X-Forwarded-For $proxy_add_x_forwarded_for;
     X-Forwarded-Proto $scheme;` and the websocket `Upgrade`/`Connection` headers.
   DNS: A records for both names to 72.60.20.70.
3. **Fill in** `stocktake-server/.env.maflam-testing` and `.env.maflam-live` (git ignores them;
   the template is `maflam.env.example`). `SSH_PRIVATE_KEY_FILE` is the path to the private key
   from step 1. Leave DB/REDIS/JWT/OTP empty: they are generated once and kept in the file.
4. **Push them to GitHub** (needs `gh auth login` as a repository admin):
   ```powershell
   pwsh deploy/maflam/setup-github.ps1 -DryRun   # names only, changes nothing
   pwsh deploy/maflam/setup-github.ps1           # creates maflam-testing / maflam-live and sets everything
   ```
   Run it again after changing a value; it updates in place.
5. Commit and push the workflow. The first push to `main` deploys testing.

## Day to day

- **Testing**: push to `main`, or Actions → *Deploy Maflam Trading (Vantage)* → Run workflow → `testing`.
- **Live**: Actions → *Deploy Maflam Trading (Vantage)* → Run workflow → `live`.
- Logs: `ssh deploy@<box> 'cd ~/stocktake/testing && docker compose -p maflam-testing $(cat .compose-files) logs -f stocktake-api'`
  (live: `~/stocktake/live`, `-p maflam-live`).
- Database shell: `docker compose -p maflam-live $(cat .compose-files) exec stocktake-db psql -U stocktake stocktake` in that stack's directory.
- After the first deploy, sign in to the portal (`PUBLIC_URL`) as `admin` / `admin123`, **change
  the password at once**, then add the stores (store code + Vantage store no.) and Staff.

## Variables and secrets (per environment)

| Secrets | Variables |
|---|---|
| `SSH_PRIVATE_KEY`, `DB_PASSWORD`, `REDIS_PASSWORD`, `JWT_SECRET`, `OTP_SECRET`, `VANTAGE_CLIENT_SECRET`, `SMS_API_KEY` | `SSH_HOST`, `SSH_PORT`, `SSH_USER`, `SSH_KNOWN_HOSTS`, `APP_DIR`, `HTTP_PORT`, `PROXY_NETWORK`, `PROXY_LETSENCRYPT`, `PUBLIC_URL`, `PUBLIC_WS_URL`, `DEPLOYMENT_NAME`, `VANTAGE_TENANT_ID`, `VANTAGE_ENVIRONMENT`, `VANTAGE_COMPANY_ID`, `VANTAGE_CLIENT_ID`, `SMS_BASE_URL`, `SMS_SENDER`, tuning (`OTP_*`, `*_TOKEN_*`, `VARIANCE_TOLERANCE_PCT`) |

`DB_PASSWORD` must not change once a server has a database: the setup script keeps it in the
`.env.maflam-*` file; keep that file safe (it is the only copy outside GitHub's secret store).
