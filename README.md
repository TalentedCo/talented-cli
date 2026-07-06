# talented

JSON-first CLI for the Talented Agent API.

This CLI is for agents and automation working on normal employer-side recruiting workflows: companies, jobs, candidates, applications, pipeline movement, notes, status, and favorites.

It does not expose super-admin, billing, impersonation, raw database, migration, feature-flag, eval/debug, Customer.io/Dittofeed, or bulk destructive automation.

## Status

This repository targets the Agent API added in the linked `TalentedCo/talented-co` PR for FIZZY-249. Production API calls require that app PR to land.

## Install

```bash
go install github.com/TalentedCo/talented-cli@latest
```

From source:

```bash
git clone https://github.com/TalentedCo/talented-cli
cd talented-cli
go build -o ~/.local/bin/talented .
```

## Auth

Create a `tal_...` token in the Talented web app, then save it:

```bash
talented auth save --token tal_...
```

Use a non-production preview:

```bash
talented auth save --token tal_... --api-url https://talented-preview.example.com
```

Environment overrides:

```bash
export TALENTED_API_TOKEN=tal_...
export TALENTED_API_URL=https://app.talented.co
```

Profiles:

```bash
talented auth save --profile staging --token tal_... --api-url https://staging.example.com
talented auth use staging
talented auth status
talented auth list
talented --profile staging whoami
```

Tokens are saved to the OS keychain when available and fall back to `~/.config/talented/config.json` with mode `0600`.

## Commands

Every command prints JSON on stdout.

```text
talented whoami
talented agent-context

talented companies list
talented companies get --company <id>
talented companies invite --company <id> --email teammate@example.com --role ADMIN

talented jobs list --company <id> [--status ACTIVE] [--search engineer]
talented jobs get --job <id>
talented jobs create --company <id> --title "Staff Engineer" [--description "..."]
talented jobs update --job <id> --title "Principal Engineer"
talented jobs status --job <id> --status ACTIVE

talented applications list --job <id> [--limit 10] [--stage <id>] [--search ada]
talented applications get --application <id>
talented applications create --job <id> --email candidate@example.com --first-name Ada --last-name Lovelace
talented applications move --application <id> --stage <id>
talented applications reject --application <id> --reason "Not a match"
talented applications unreject --application <id>

talented candidates get --candidate <id>
talented candidates status --candidate <id> --status CONTACTED
talented candidates favorite --candidate <id> --value true
talented candidates notes list --candidate <id>
talented candidates notes add --candidate <id> --content "Follow up this week"

talented skill get talented
```

## Example Smoke

```bash
talented whoami
talented companies list
talented companies invite --company 74 --email tanya@woofiesrh.com --role ADMIN
talented jobs list --company 1
talented applications list --job 10 --limit 10
talented candidates notes add --candidate 20 --content "Follow up this week"
talented applications move --application 30 --stage 40
```

`companies invite` only invites or adds a user to an existing company visible to
your token. It never creates companies as a fallback and requires `agent:write`
plus company owner/admin permissions. `--role` must be `ADMIN` or `MEMBER`.

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | success |
| `1` | generic error |
| `64` | usage error |
| `65` | validation/API input error |
| `69` | network error |
| `70` | server error |
| `73` | conflict |
| `74` | not found |
| `77` | auth error |

## Development

```bash
mise exec go@1.26.2 -- go mod tidy
mise exec go@1.26.2 -- go test ./...
mise exec go@1.26.2 -- go build ./...
```
