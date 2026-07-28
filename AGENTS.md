# AGENTS.md

## Cursor Cloud specific instructions

This is a single-binary Go project (Discord Auto Translator). Architecture, config, and
testing conventions are documented in `docs/DEV_NOTES.md` and `README.md`; the CI pipeline
lives in `.github/workflows/ci.yml`. Only the non-obvious, cloud-specific caveats are below.

### Toolchain
- Requires **Go 1.24** (see `go.mod`). The distro's apt Go is 1.22 and is too old — it cannot
  even build the module. Go 1.24 is installed at `/usr/local/go` and symlinked into
  `/usr/local/bin`, so `go` on `PATH` already resolves to 1.24. Verify with `go version`.
- SQLite uses `modernc.org/sqlite` (pure Go), so **no CGO / C compiler** is needed.

### Required pre-build step (gitignored files)
- The legal pages (`internal/legalpages/assets/privacy.html` and `terms.html`) are gitignored
  and are embedded at build time. `go vet`, `go test`, and `go build` will fail unless the
  `.example` placeholders are copied into place first. The startup update script does this
  idempotently; CI does the same copy step.

### Lint / test / build / run
- Lint: `go vet ./...`
- Test: `go test ./...` (uses in-memory SQLite + mocked Discord/Bedrock — no secrets needed).
- Build: `go build -o discord-auto-translator ./cmd/discord-auto-translator`
- Run: `go run ./cmd/discord-auto-translator` (reads `.env`; see `.env.example`).

### Running the full bot needs real external credentials
- Starting the live bot requires real secrets: `DISCORD_TOKEN` plus Bedrock credentials
  (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_BEDROCK_REGION`, `AWS_BEDROCK_PROJECT_ID`).
  See `.env.example` and `docs/DEV_NOTES.md` §9 for the full list.
- Without them the binary fails fast during config validation; with fake-but-well-formed values
  it passes config and then hits a real Discord `/users/@me` call that returns HTTP 401. This is
  expected and confirms the startup path runs end to end.
- `go run ./cmd/discord-auto-translator --bedrock-prewarm` validates Bedrock model access and the
  response contract only (no Discord/SQLite/HTTP), and needs valid Bedrock credentials.
- `DISCORD_TOKEN` also needs the privileged **MESSAGE CONTENT** intent enabled in the Discord
  Developer Portal for message translation to work.
