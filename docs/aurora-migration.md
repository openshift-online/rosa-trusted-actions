# Aurora PostgreSQL Migration

Phase 1 of the ECS deployment runs a single task backed by SQLite on an EBS
volume. That design pins the task to one availability zone, caps the service at
`desired_count = 1`, and has no managed backups. Phase 2 replaces it with Aurora
PostgreSQL on Fargate.

Tracked by [ROSAENG-67617](https://redhat.atlassian.net/browse/ROSAENG-67617)
under epic [ROSAENG-59927](https://redhat.atlassian.net/browse/ROSAENG-59927).
The application-side work (`internal/store/postgres.go`, Postgres migrations)
landed under [ROSAENG-67173](https://redhat.atlassian.net/browse/ROSAENG-67173)
and is already merged.

---

## Table of Contents

1. [Decisions](#decisions)
2. [Implementation Sequence](#implementation-sequence)
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

### RDS manages the master password; the application composes the DSN

The cluster sets `manage_master_user_password = true`. RDS generates the
password and stores it in a Secrets Manager secret it owns, named
`rds!cluster-<uuid>-<suffix>`, holding JSON `{"username": ..., "password": ...}`.
No password appears in Terraform configuration or state.

The application takes a single `DATABASE_URL` today
(`internal/config/config.go`, `cmd/server/main.go`), so the DSN has to be
assembled somewhere. It is assembled **in the application**:

| Source             | Variables                                                                  |
|--------------------|----------------------------------------------------------------------------|
| `environment`      | `ROSA_TA_DB_HOST`, `ROSA_TA_DB_PORT`, `ROSA_TA_DB_NAME`, `ROSA_TA_DB_USER`, `ROSA_TA_DB_SSLMODE` |
| `secrets`          | `ROSA_TA_DB_PASSWORD` from `"<master_user_secret_arn>:password::"`           |

`config.go` builds the DSN from these when `DATABASE_URL` is unset, using
`url.UserPassword()` rather than string concatenation — RDS generates 28-character
passwords containing punctuation, and a raw `%` or `:` in a hand-built URL
corrupts the DSN silently.

Two alternatives were rejected:

- **Terraform composes the DSN** into a secret of our own, reading the
  RDS-owned secret through a data source. No application change, but the
  composed DSN is a *copy* — the first rotation makes it stale and the service
  cannot connect. It adopts the managed-secret machinery while guaranteeing it
  breaks.
- **`random_password` into a Terraform-authored secret**, matching the existing
  pattern in `terraform/global/secrets.tf`. Consistent and simple, but there is
  no managed rotation and the password lands in state.

### Rotation drives an ECS redeployment

Aurora rotates the managed master secret **every seven days by default**. The
old password stops working immediately. The ECS agent resolves `secrets` once,
at task launch, and injects them as process environment — there is no live
re-read. Running tasks therefore hold a dead password, and because
`ROSA_TA_DB_CONN_MAX_LIFETIME` defaults to 5 minutes the pool recycles and fails
to reconnect within minutes of every rotation.

An EventBridge rule on the Secrets Manager rotation event forces a new
deployment of the ECS service. At `desired_count = 2` that is a rolling restart
with no downtime. The rotation window is set to a quiet hour.

This needs more than a rule. EventBridge has **no native "update ECS service"
target** — ECS targets run tasks, they do not update services — so a small
handler (most likely a Lambda calling `ecs:UpdateService` with
`forceNewDeployment`) sits between the two. Two further details are unconfirmed:
the exact `detail-type` of the rotation event for an RDS-*managed* master
secret, and whether the rotation schedule on an RDS-owned secret can be managed
from Terraform without fighting RDS over ownership. Because of that uncertainty
the handler is its own ticket rather than part of the cutover.

Nothing recovers on its own in the meantime. `/health` (`cmd/server/main.go`)
is a static 200 that never touches the database, so after a rotation both tasks
stay healthy to the ALB while every request reaching the store fails, and ECS
does not replace them. Recovery is a human running
`aws ecs update-service --force-new-deployment`.

Stretching the rotation schedule to a long interval as a bridge was considered
and rejected. It is managed rotation in name only, it leaves a scheduled outage
armed behind a config value that someone will one day tidy up, and it provides
no feedback that anything is outstanding. Instead the handler is sequenced to
start as soon as the cluster exists, so it lands at or near the cutover, and
staging simply absorbs a recoverable blip if a rotation beats it. That trade is
only acceptable because staging's database is disposable — see the production
gate below.

### The rotation handler gates production

**This design must not be deployed to production until the rotation handler
exists.** In staging an unhandled rotation is a two-minute manual fix against
data nobody depends on. In production the same gap is a seven-day repeating
outage.

### The secrets IAM grant uses a wildcard in the global workspace

Secrets Manager and the `read_secrets` policy live in `terraform/global/`;
Aurora lives in `terraform/regional/`; the dependency runs global → regional via
`terraform_remote_state` and a run trigger. Global can never see the Aurora
endpoint or the RDS-owned secret ARN.

`read_secrets` in `terraform/global/iam.tf` therefore gains a second Resource
entry:

```
arn:aws:secretsmanager:*:<ACCOUNT>:secret:rds!cluster-*
```

This follows the cross-region wildcard pattern already documented in that file's
header for the Firehose and CloudWatch Logs policies. The cluster UUID is not
predictable at policy-write time, so the wildcard covers any RDS-managed cluster
secret in the account. If the account grows other clusters, tighten it with a
`secretsmanager:ResourceTag` condition.

No DSN secret of our own is created — the task reads only the RDS-owned secret.

### EC2 bridge networking becomes Fargate awsvpc

`desired_count = 2` is impossible while the task is pinned to an AZ by an EBS
volume attached to a single EC2 host. The cutover moves the task definition to
`network_mode = "awsvpc"` with `launch_type = "FARGATE"`, switches the ALB
target group to `target_type = "ip"`, and deletes the host.

- **Task sizing**: Fargate requires task-level `cpu` and `memory` from a fixed
  table of valid pairings. `1024` / `2048` is the smallest pairing that fits the
  existing 768 cpu / 768 MB of container reservations with headroom for the
  Postgres connection pool. Exposed as `task_cpu` / `task_memory`.
- **Deployment**: `minimum_healthy_percent` moves from `0` to `100` and
  `maximum_percent` from `100` to `200`. The current values mean every deploy is
  a full outage — correct for one task on one host, wrong at `desired_count = 2`.
- **Grace period**: `health_check_grace_period_seconds = 60`, because the task
  runs database migrations before it serves `/health`.
- **Shell access**: `enable_execute_command = true`. With the EC2 host gone,
  SSM session access to the box goes with it, and ECS Exec is the only remaining
  way to get a shell next to the database.
- **`config-init` is unaffected.** Fargate supports multi-container tasks,
  shared volumes, and `dependsOn: SUCCESS` on a non-essential container, and the
  NAT egress that lets the EC2 host pull from quay.io works identically for
  `public.ecr.aws`.
- **The ALB health check is unchanged.** `/health` on port 8080 works against
  task ENIs exactly as it does against instances.

### Concurrent migrations are already safe

`internal/store/migrations.go` takes a `pg_advisory_lock` around the migration
loop for the Postgres dialect, so two tasks booting simultaneously serialise.
`desired_count = 2` needs no further application work.

### No data migration

Staging execution history and audit entries are disposable, and the S3
audit-log path is the record of truth for anything that matters. The EBS volume
is deleted, not drained. If this is ever repeated in an environment with data
worth keeping, snapshot the volume first and treat the migration as its own
ticket.

### `sslmode=require` now, `verify-full` later

`require` encrypts the connection but does not verify the server certificate.
`verify-full` needs the RDS CA bundle present in the container image, which the
`Containerfile` does not ship today. The connection never leaves the VPC and the
Aurora security group admits only the ECS task security group, so the exposure
is an attacker already inside the private subnets. Tracked as its own story.

---

## Implementation Sequence

Three pull requests plus a trailing cleanup, each applied and reviewed on its
own HCP Terraform plan. One PR with three commits would collapse into a single
plan and a single apply, which defeats the point: a failed apply would leave the
deployment with neither a working EBS host nor a working Fargate task.

| # | Change                                | Root                  | Blocked by |
|---|---------------------------------------|-----------------------|------------|
| 1 | Aurora cluster + IAM grant            | regional + global     | —          |
| 2 | Application composes the DSN          | `internal/config/`    | —          |
| 3 | Fargate cutover                       | regional              | 1, 2       |
| 4 | Regional EC2/EBS teardown             | regional              | 3          |
| 5 | Global instance-profile teardown      | global                | 4          |
| 6 | Rotation → forced ECS redeployment    | regional              | 1          |
| 7 | `sslmode=verify-full`                 | `Containerfile` + regional | 3     |

Changes 1–5 are sub-tasks of ROSAENG-67617. Changes 6 and 7 are sibling stories
under epic ROSAENG-59927 — both are follow-ups to the cutover rather than part
of it.

### 1 — Aurora cluster and IAM grant

New `terraform/regional/rds.tf`:

- `aws_rds_cluster` — `aurora-postgresql`, pinned engine version,
  `manage_master_user_password = true`, `master_username = var.db_username`,
  `database_name = var.db_name`, `storage_encrypted = true`, backup retention
  and window, deletion protection and final-snapshot flags from variables.
- `aws_rds_cluster_instance` — `count = var.db_instance_count` (2), spread
  across both private subnets.
- `aws_db_subnet_group` — both private subnets.

`terraform/regional/variables.tf` gains the `db_*` variables listed under
[Decisions](#aurora-provisioned-not-serverless-v2).

`terraform/global/iam.tf` — add the `rds!cluster-*` Resource to
`aws_iam_policy.read_secrets`.

The security groups already exist: `aws_security_group.aurora` and
`aws_security_group.ecs_task` in `terraform/regional/security_groups.tf`, with
ingress on 5432 from the task security group already wired.

Aurora comes up idle. The application is untouched and still on SQLite, so this
change is additive and independently applyable.

### 2 — Application composes the DSN

`internal/config/config.go` — read `ROSA_TA_DB_HOST`, `ROSA_TA_DB_PORT`,
`ROSA_TA_DB_NAME`, `ROSA_TA_DB_USER`, `ROSA_TA_DB_PASSWORD`,
`ROSA_TA_DB_SSLMODE`, and compose a `postgres://` DSN with `url.UserPassword()`
when `DATABASE_URL` is unset. `DATABASE_URL`, when set, continues to win —
local development and the existing test setup depend on it.

Unit tests cover the composed DSN, precedence over `DATABASE_URL`, and
percent-escaping of a password containing punctuation.

This blocks change 3, not change 1 — Aurora can be provisioned before the
application knows how to reach it; what it cannot precede is the cutover.

### 3 — Fargate cutover

`terraform/regional/ecs.tf`:

- `network_mode = "awsvpc"`, `requires_compatibilities = ["FARGATE"]`,
  task-level `cpu` / `memory` from `var.task_cpu` / `var.task_memory`.
- `network_configuration` on the service referencing
  `aws_security_group.ecs_task` and both private subnets.
- `launch_type = "FARGATE"`, `desired_count = 2`,
  `deployment_minimum_healthy_percent = 100`,
  `deployment_maximum_percent = 200`,
  `health_check_grace_period_seconds = 60`, `enable_execute_command = true`.
  Remove `ordered_placement_strategy` — it is not valid for Fargate.
- Remove the `sqlite-data` volume and its `/data` mount point.
- Remove `DATABASE_URL` from `container_environment`; add the `ROSA_TA_DB_*`
  entries to `container_environment` and `ROSA_TA_DB_PASSWORD` to
  `container_secrets`.

`terraform/regional/alb.tf` — `target_type = "ip"`.

Leave the rotation schedule at its default. Change 6 handles rotation and runs
in parallel with this one; if a rotation lands first, staging is recovered with
a manual forced deployment.

### 6 — Rotation triggers a forced ECS redeployment

An EventBridge rule on the rotation event and a handler that forces a new
deployment. Carries real investigation: the event's `detail-type` for
RDS-managed secrets, whether the schedule is Terraform-manageable against an
RDS-owned secret, whether a Lambda is avoidable in a repository that has none,
and whether the event reliably follows the password change rather than
preceding it.

**Blocked by change 1, not change 3.** The research needs a cluster whose
secret can be rotated on demand, not Fargate — so this runs in parallel with
changes 2 and 3 and should land at or near the cutover. Only the final
no-downtime verification needs `desired_count = 2`.

A sibling story under the epic, not a sub-item of ROSAENG-67617.

### 4 — Regional EC2/EBS teardown

Delete `ebs.tf`, `ec2.tf`, `templates/userdata.sh.tpl`,
`aws_security_group.ec2` and its `alb_to_ec2` / `ec2_from_alb` rules,
`var.instance_type`, `data.aws_subnet.private_a`, and
`local.private_subnet_a_az` in `vpc.tf`.

Gated on staging running stably on Fargate — this is the irreversible step.

### 5 — Global instance-profile teardown

Delete `aws_iam_role.ecs_instance`, its two policy attachments,
`aws_iam_instance_profile.ecs_instance`, and the `iam_instance_profile_name`
output.

**Ordering matters.** Change 4 must land first. Removing that output while the
regional state still reads `local.global.iam_instance_profile_name` breaks every
regional plan.

### 7 — `sslmode=verify-full`

Add the RDS CA bundle to the `Containerfile` and `sslrootcert` to the composed
DSN. A sibling story under the epic, not a sub-item of ROSAENG-67617.

---

## Known Next Steps

Neither is scheduled.

**IAM database authentication.** The application would mint a 15-minute auth
token per connection via `rds.GenerateDBAuthToken` in a pgx `BeforeConnect`
hook, plus `rds-db:connect` in IAM and an `rds_iam` grant on the database role.
No password in the application path at all, and no rotation problem to solve.
Password authentication works correctly with the design above — this would
replace a working mechanism, so it belongs to a deliberate security initiative
rather than this migration.

**Aurora observability.** Nothing in `cloudwatch.tf` watches a database.
Candidates: CPU, freeable memory, connection count versus pool size, replica
lag, and storage. Deliberately out of scope here.

---

## Corrections to ROSAENG-67617

Three statements in the ticket description do not match the repository:

- There are no `TODO` markers in `ecs.tf`, `iam.tf`, or `variables.tf`, and
  there is no `terraform/regional/iam.tf` — IAM lives in `terraform/global/`.
- `ebs.tf` has no `prevent_destroy` lifecycle block, commented or otherwise.
- The `ecs_task` and `aurora` security groups already exist in
  `security_groups.tf`, with Aurora ingress on 5432 from the task security
  group already wired.
