# Talented Agent Skill

Use `talented` for employer-side recruiting work in Talented.

## Setup

1. Ask the human to create a Talented API token in the web app.
2. Save it locally:

```bash
talented auth save --token tal_...
```

3. Verify:

```bash
talented whoami
talented agent-context
```

## Safe Scope

You may list accessible companies, list and inspect jobs, list and inspect candidates/applications, create a single candidate/application, move a single application to a valid stage, reject or unreject one application, update candidate status/favorite, and add candidate notes.

Do not attempt super-admin, impersonation, billing, raw database, migration, feature-flag, eval/debug, or bulk destructive work through this CLI.

## Useful Commands

```bash
talented companies list
talented jobs list --company <company_id>
talented applications list --job <job_id> --limit 10
talented applications move --application <application_id> --stage <stage_id>
talented candidates notes add --candidate <candidate_id> --content "Follow up this week"
```
