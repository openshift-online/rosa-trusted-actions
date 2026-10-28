# Database and Compute

How the ROSA Trusted Actions service stores its data and where it runs: Aurora
PostgreSQL behind an ECS service on Fargate.

Tracked by [ROSAENG-67617](https://redhat.atlassian.net/browse/ROSAENG-67617)
under epic [ROSAENG-59927](https://redhat.atlassian.net/browse/ROSAENG-59927).
The application-side store work (`internal/store/postgres.go`, Postgres
migrations) landed under
[ROSAENG-67173](https://redhat.atlassian.net/browse/ROSAENG-67173).

> **This is a greenfield build, not a migration.** The deployment was originally
> designed in two phases — SQLite on an EBS volume first, Aurora second — but
> Phase 1 was never deployed. The regional Terraform root has never been
> applied in any environment, so the EBS, EC2 and SQLite resources exist only
> as HCL. They are deleted as part of writing the target state. There is no
> volume to drain, no host to retire, and no running service to cut over.

---

## Table of Contents

1. [Decisions](#decisions)
2. [Build Sequence](#build-sequence)
3. [Known Next Steps](#known-next-steps)

---

## Decisions

### Aurora provisioned, not Serverless v2

Two `db.t4g.medium` instances — a writer and a reader in the second AZ — satisfy
the Multi-AZ requirement. `db.t4g.medium` is the smallest class Aurora
PostgreSQL offers. Serverless v2 would idle cheaper in staging, but provisioned
capacity is predictable and the same `aws_rds_cluster_instance` resource covers
both, so switching later is a one-line change.

Every tunable is a variable with a staging-appropriate default:
`db_instance_class`, `db_instance_count`, `db_engine_version`,
`db_backup_retention_period`, `db_preferred_backup_window`,
`db_deletion_protection`, `db_skip_final_snapshot`,
`db_performance_insights_enabled`, `db_kms_key_id`. `db_username` and `db_name`
already exist in `terraform/regional/variables.tf`.

The engine version is pinned explicitly. A floating minor version means an
unplanned failover during an AWS maintenance window; a version bump should be a
deliberate PR.

> Verify before the first apply that the chosen class is orderable for the
> chosen engine version and region — t4g availability varies:
>
> ```bash
> aws rds describe-orderable-db-instance-options \
>   --engine aurora-postgresql --engine-version <ver> --region us-east-1
> ```

### The password is authored in Vault; the application composes the DSN

The cluster deliberately does **not** set `manage_master_user_password`. RDS
would generate and own the password — nothing in configuration or state — but
it also rotates that secret every seven days, and rotation cannot be switched
off while keeping it. See
[Rotation is disabled](#rotation-is-disabled-deliberately).

Instead the password is generated and held in our external Vault, supplied to
the regional workspace as the sensitive variable `db_password`
(`terraform/regional/variables.tf`), and written to a Secrets Manager secret of
our own — `<APP_NAME>/db`, in `terraform/regional/secrets.tf` — holding JSON
`{"username": ..., "password": ...}`. That is the same shape an RDS-managed
secret produces, so adopting one later changes nothing downstream.

**The password therefore appears in Terraform state**, both in the regional
workspace and in the local state of the workspace-management root under
`hcp-terraform/`. This is accepted rather than overlooked; the reasoning is in
[Rotation is disabled](#rotation-is-disabled-deliberately).

**Generating it.** Nothing ever types this password, so generate it rather than
choosing one. Aurora PostgreSQL rejects `/`, `"`, `'`, `@` and spaces in a
master password; `db_password` is validated against exactly those, so a bad
value fails at plan time rather than at apply time. The set below also omits
characters that are hazardous to paste into a shell (`$`, `` ` ``, `\`, `!`,
`&`, `|`, `<`, `>`, parentheses), which `-var="db_password=…"` makes a real
risk:

```bash
LC_ALL=C tr -dc 'A-Za-z0-9#%*+,-.:;=?^_~' </dev/urandom | head -c 32; echo
```

`%` and `:` are safe in the DSN despite their meaning in a URL — `config.go`
uses `url.UserPassword()`, which escapes them.

**Retrieving it.** Vault is the system of record and the only place a human can
read this password. The Secrets Manager copy is not reachable by hand: nobody
holds AWS credentials for the account (see
[Rotation is disabled](#rotation-is-disabled-deliberately)). Storing it only in
Terraform state would make it unreadable by anyone, which is why Vault was
chosen over `random_password`.

> **Vault path: TBD.** Fill in the mount and path before the first apply —
> a credential whose location is undocumented is not recoverable.

**One password per cluster.** Each region has its own Aurora cluster and must
get its own generated value; do not reuse one across regions.

The application takes a single `DATABASE_URL`
(`internal/config/config.go`, `cmd/server/main.go`), so the DSN has to be
assembled somewhere. It is assembled **in the application**:

| Source             | Variables                                                                  |
|--------------------|----------------------------------------------------------------------------|
| `environment`      | `ROSA_TA_DB_HOST`, `ROSA_TA_DB_PORT`, `ROSA_TA_DB_NAME`, `ROSA_TA_DB_USER`, `ROSA_TA_DB_SSLMODE`, `ROSA_TA_DB_SSLROOTCERT` |
| `secrets`          | `ROSA_TA_DB_PASSWORD` from `"<APP_NAME>/db:password::"`                     |

`config.go` builds the DSN from these when `DATABASE_URL` is unset, using
`url.UserPassword()` rather than string concatenation — the password is a long
random string containing punctuation, and a raw `%` or `:` in a hand-built URL
corrupts the DSN silently. `db_password` is validated in
`terraform/regional/variables.tf` against the characters Aurora PostgreSQL
itself rejects in a master password (`/`, `"`, `'`, `@`, space), so a bad value
fails at plan time rather than at apply time.

Two alternatives were rejected:

- **Terraform composes the whole DSN** into a secret, rather than passing only
  the password. The application already assembles the DSN from components
  (ROSAENG-67173), and a composed DSN would duplicate the host, port and
  database name that Terraform already passes as plain environment variables.
- **`random_password` instead of a Vault-authored value**, matching the
  existing pattern in `terraform/global/secrets.tf`. It avoids a manual step,
  but a generated password exists *only* in Terraform state — nobody can
  retrieve it. Combined with the fact that no human holds credentials for the
  AWS account, that would leave the database credential unreadable by anyone.
  Vault keeps it recoverable, which is the whole reason to prefer it.

### Rotation is disabled, deliberately

There is no automatic rotation of the Aurora master password. This is a
deliberate interim position with a ticket against it:
[ROSAENG-74839](https://redhat.atlassian.net/browse/ROSAENG-74839).

**Why rotation had to go.** The ECS agent resolves a task's `secrets` once, at
launch, and injects them as process environment — there is no live re-read. A
rotation therefore leaves running tasks holding a dead password, and because
`ROSA_TA_DB_CONN_MAX_LIFETIME` defaults to 5 minutes the pool recycles and
fails to reconnect within minutes. Nothing recovers on its own: `/health`
(`cmd/server/main.go`) is a static 200 that never touches the database, so both
tasks stay healthy to the ALB while every request reaching the store fails, and
ECS does not replace them. Recovery would be a human running
`aws ecs update-service --force-new-deployment`.

**Why it is disabled rather than handled.** Three routes were examined:

- **An EventBridge rule driving a forced ECS deployment.** EventBridge has no
  native "update ECS service" target — ECS targets run tasks, they do not
  update services — so this needs a Lambda calling `ecs:UpdateService`, the
  first in this repository. It also cannot close the window: between the
  password changing and replacement tasks passing health checks there are two
  to four minutes in which any new connection fails.
- **Turning rotation off while keeping `manage_master_user_password`.** AWS
  documents no way to do this. The Terraform-native route —
  `aws_secretsmanager_secret_rotation` with `rotation_enabled = false` —
  requires AWS provider **≥ 6.62**, and both roots pin `~> 5.0`. The CLI route,
  `aws secretsmanager cancel-rotate-secret`, cannot be used at all: the HCP
  workspaces authenticate with AWS dynamic credentials minted per run and, by
  design, no human holds credentials for the account. **Anything that is not a
  Terraform resource cannot be executed against this infrastructure.**
- **Stretching the rotation schedule** to a long interval. Rejected: it is
  managed rotation in name only, it leaves a scheduled outage armed behind a
  config value that someone will one day tidy up, and it provides no feedback
  that anything is outstanding.

**IAM database authentication is the intended fix.** It removes the password
from the application path entirely rather than managing its rotation. It is
deferred because it needs a Go change, the repository's first AWS SDK
dependency, and a separate application role — AWS documents that granting
`rds_iam` to a user forces IAM-only login *including for the master user*, so
the master user cannot also be the IAM user. Tracked on ROSAENG-74839.

**Consequences to know about.**

- **Changing `db_password` rotates the master password** on the next apply and
  breaks every running task. A forced ECS deployment must follow. The cluster
  and the Secrets Manager copy are deliberately *not* decoupled with
  `ignore_changes`: divergence between them would crash-loop every new task on
  a password the database never accepted, which is worse than a known restart.
- **No human network path to the database exists.** Aurora is in private
  subnets, deleting the EC2 configuration removed the only in-VPC host anyone
  could have reached, and the runtime image (`core-runtime`) ships no `psql`.
  Knowing the password is not currently enough to reach the database. A
  one-off `aws ecs run-task` on a `postgres` image in the task security group
  is the likely answer. Also tracked on ROSAENG-74839.
- **A production credential that never rotates may be an audit finding.** This
  was accepted knowingly rather than discovered.

### The secrets IAM grant uses a wildcard in the global workspace

Secrets Manager and the `read_secrets` policy live in `terraform/global/`; the
Aurora credentials secret lives in `terraform/regional/`; the dependency runs
global → regional via `terraform_remote_state` and a run trigger. Global can
never see the regional secret's full ARN.

`read_secrets` in `terraform/global/iam.tf` therefore gains a second Resource
entry:

```
arn:aws:secretsmanager:*:<ACCOUNT>:secret:<APP_NAME>/db-*
```

This follows the cross-region wildcard pattern already documented in that file's
header for the Firehose and CloudWatch Logs policies. The name is ours and
predictable; the trailing `-*` covers only the random six-character suffix
Secrets Manager appends when it creates a secret.

### Fargate, not EC2

The original design put the task on an EC2 host in `bridge` network mode, with
an EBS volume attached for the SQLite file. That pinned the task to one
availability zone and capped the service at `desired_count = 1`. With Aurora
there is no local state, so there is no reason to manage a host.

- **Task sizing**: Fargate requires task-level `cpu` and `memory` from a fixed
  table of valid pairings. `1024` / `2048` is the smallest pairing that fits the
  existing 768 cpu / 768 MB of container reservations with headroom for the
  Postgres connection pool. Exposed as `task_cpu` / `task_memory`.
- **Deployment**: `deployment_minimum_healthy_percent = 100` and
  `deployment_maximum_percent = 200`. The Phase 1 values (`0` / `100`) meant
  every deploy was a full outage — tolerable for one task on one host, wrong at
  `desired_count = 2`.
- **Grace period**: `health_check_grace_period_seconds = 60`, because the task
  runs database migrations before it serves `/health`.
- **Shell access**: `enable_execute_command = true`. With no EC2 host there is
  no SSM session into a box, and ECS Exec is the only way to get a shell next to
  the database.
- **`config-init` is unaffected.** Fargate supports multi-container tasks,
  shared volumes, and `dependsOn: SUCCESS` on a non-essential container, and NAT
  egress works identically for `public.ecr.aws` as for quay.io.
- **The ALB target group uses `target_type = "ip"`.** The `/health` check on
  port 8080 works against task ENIs exactly as it would against instances.

### Concurrent migrations are already safe

`internal/store/migrations.go` takes a `pg_advisory_lock` around the migration
loop for the Postgres dialect, so two tasks booting simultaneously serialise.
`desired_count = 2` needs no further application work.

### TLS is verified end to end

The DSN uses `sslmode=verify-full`, which checks that the Aurora server
certificate is signed by a trusted CA and that the hostname matches. The
`Containerfile` ships the AWS global RDS trust bundle
(`https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem`) and
`ROSA_TA_DB_SSLROOTCERT` points the DSN at it.

`sslmode=require` — encrypt but do not verify — was the original plan, on the
grounds that adding the CA bundle mid-cutover was extra risk. With nothing
deployed there is no cutover and no reason to ship the weaker setting first.

AWS rotates these CAs on a published schedule, so the bundle is fetched at build
time rather than committed, and the renewal path is a rebuild.

---

## Build Sequence

| # | Change                                          | Scope                                | Blocked by |
|---|-------------------------------------------------|--------------------------------------|------------|
| 1 | Application composes a verifying DSN            | `internal/config/`, `Containerfile`  | —          |
| 2 | Aurora cluster + IAM grant                      | regional + global                    | —          |
| 3 | Fargate service; delete EC2/EBS configuration   | regional + global                    | 1, 2       |

Changes 2 and 3 are commits in a single PR and apply together. Applying 2 alone,
with `ebs.tf` and `ec2.tf` still present, would create an EC2 host and a 20 GB
EBS volume on the first apply — which 3 would immediately destroy.

Change 1 must merge and produce an image before the first apply, because
`container_image` is pinned by digest in the HCP workspace variables.

A fourth change — an EventBridge rule and handler forcing an ECS redeployment
on rotation — was planned and then dropped once rotation was disabled outright.
See [Rotation is disabled](#rotation-is-disabled-deliberately) and
ROSAENG-74839.

### 1 — Application composes a verifying DSN

`internal/config/config.go` reads `ROSA_TA_DB_HOST`, `ROSA_TA_DB_PORT`,
`ROSA_TA_DB_NAME`, `ROSA_TA_DB_USER`, `ROSA_TA_DB_PASSWORD`,
`ROSA_TA_DB_SSLMODE` and `ROSA_TA_DB_SSLROOTCERT`, and composes a `postgres://`
DSN with `url.UserPassword()` when `DATABASE_URL` is unset. `DATABASE_URL`, when
set, continues to win — local development, `compose.postgres.yml`, and the
existing test setup all depend on it.

The `Containerfile` fetches the RDS CA bundle to a documented path.

### 2 — Aurora cluster and IAM grant

New `terraform/regional/rds.tf`:

- `aws_rds_cluster` — `aurora-postgresql`, pinned engine version,
  `master_password = var.db_password`, `master_username = var.db_username`,
  `database_name = var.db_name`, `storage_encrypted = true`, backup retention
  and window, deletion protection and final-snapshot flags from variables.
- `aws_rds_cluster_instance` — `count = var.db_instance_count` (2), spread
  across both private subnets.
- `aws_db_subnet_group` — both private subnets.

New `terraform/regional/secrets.tf` — `aws_secretsmanager_secret` named
`<APP_NAME>/db` and its version, holding `{username, password}`. No
`ignore_changes` on the version; it must track `master_password`.

`terraform/regional/variables.tf` gains the `db_*` variables listed under
[Decisions](#aurora-provisioned-not-serverless-v2), plus the sensitive,
validated `db_password`.

`terraform/global/iam.tf` — add the `<APP_NAME>/db-*` Resource to
`aws_iam_policy.read_secrets`.

`hcp-terraform/rosa-trusted-actions/` — `db_password` as a sensitive workspace
variable on the regional workspace, alongside the existing three.

The security groups already exist: `aws_security_group.aurora` and
`aws_security_group.ecs_task` in `terraform/regional/security_groups.tf`, with
ingress on 5432 from the task security group already wired.

### 3 — Fargate service, EC2/EBS configuration deleted

`terraform/regional/ecs.tf`:

- `network_mode = "awsvpc"`, `requires_compatibilities = ["FARGATE"]`,
  task-level `cpu` / `memory` from `var.task_cpu` / `var.task_memory`.
- `network_configuration` on the service referencing
  `aws_security_group.ecs_task` and both private subnets.
- `launch_type = "FARGATE"`, `desired_count = 2`, deployment percentages
  100/200, 60s health-check grace, `enable_execute_command = true`. Remove
  `ordered_placement_strategy` — Fargate rejects it.
- Remove the `sqlite-data` volume and its `/data` mount point.
- Replace `DATABASE_URL` with the `ROSA_TA_DB_*` environment entries, and add
  `ROSA_TA_DB_PASSWORD` to `container_secrets`.

`terraform/regional/alb.tf` — `target_type = "ip"`.

Deleted from `terraform/regional/`: `ebs.tf`, `ec2.tf`,
`templates/userdata.sh.tpl`, `aws_security_group.ec2` and its two rules,
`var.instance_type`, `local.private_subnet_a_az`.

Deleted from `terraform/global/`: `aws_iam_role.ecs_instance`, its two policy
attachments, `aws_iam_instance_profile.ecs_instance`,
`data.aws_iam_policy_document.ec2_assume`, and the `iam_instance_profile_name`
output. These land in the same commit as the regional deletions, so the output
and the reference to it disappear together and the two roots cannot be out of
step.

---

## Known Next Steps

**IAM database authentication — scheduled, on
[ROSAENG-74839](https://redhat.atlassian.net/browse/ROSAENG-74839).** The
application would mint a 15-minute auth token per connection via
`feature/rds/auth.BuildAuthToken` (the v2 SDK; `rds.GenerateDBAuthToken` is v1)
in a pgx `BeforeConnect` hook, plus `rds-db:connect` in IAM and an `rds_iam`
grant on the database role. No password in the application path at all, and no
rotation problem to solve — which is why it is now the intended fix rather than
a someday item. It needs a **separate application role**: AWS documents that
granting `rds_iam` makes IAM authentication take precedence over password
authentication *including for the master user*, so the master user cannot also
be the IAM user.

**A database-aware health check.** `/health` is a static 200. A permanent
database failure therefore leaves both tasks in the load balancer serving
errors indefinitely. A check that fails specifically on Postgres authentication
error `28P01` — not on general database unavailability, which would cascade
every task out of service during an Aurora failover — would close that gap and
would also surface a wrong `db_password` rather than leaving it to crash-loops
at task start.

**Aurora observability.** Nothing in `cloudwatch.tf` watches a database.
Candidates: CPU, freeable memory, connection count versus pool size, replica
lag, and storage. Deliberately out of scope.

---

## Corrections to ROSAENG-67617

Statements in the original ticket description that do not match the repository:

- There are no `TODO` markers in `ecs.tf`, `iam.tf`, or `variables.tf`, and
  there is no `terraform/regional/iam.tf` — IAM lives in `terraform/global/`.
- `ebs.tf` has no `prevent_destroy` lifecycle block, commented or otherwise.
- The `ecs_task` and `aurora` security groups already exist in
  `security_groups.tf`, with Aurora ingress on 5432 from the task security
  group already wired.
- The note about keeping `enable_worm = false` until the EBS→Aurora migration
  is validated is moot — there is no migration.
