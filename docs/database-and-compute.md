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

### RDS manages the master password; the application composes the DSN

The cluster sets `manage_master_user_password = true`. RDS generates the
password and stores it in a Secrets Manager secret it owns, named
`rds!cluster-<uuid>-<suffix>`, holding JSON `{"username": ..., "password": ...}`.
No password appears in Terraform configuration or state.

The application takes a single `DATABASE_URL`
(`internal/config/config.go`, `cmd/server/main.go`), so the DSN has to be
assembled somewhere. It is assembled **in the application**:

| Source             | Variables                                                                  |
|--------------------|----------------------------------------------------------------------------|
| `environment`      | `ROSA_TA_DB_HOST`, `ROSA_TA_DB_PORT`, `ROSA_TA_DB_NAME`, `ROSA_TA_DB_USER`, `ROSA_TA_DB_SSLMODE`, `ROSA_TA_DB_SSLROOTCERT` |
| `secrets`          | `ROSA_TA_DB_PASSWORD` from `"<master_user_secret_arn>:password::"`           |

`config.go` builds the DSN from these when `DATABASE_URL` is unset, using
`url.UserPassword()` rather than string concatenation — RDS generates
28-character passwords containing punctuation, and a raw `%` or `:` in a
hand-built URL corrupts the DSN silently.

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

Nothing recovers on its own. `/health` (`cmd/server/main.go`) is a static 200
that never touches the database, so after a rotation both tasks stay healthy to
the ALB while every request reaching the store fails, and ECS does not replace
them. Recovery is a human running
`aws ecs update-service --force-new-deployment`.

The fix is an EventBridge rule on the rotation event driving a forced
deployment of the ECS service. At `desired_count = 2` that is a rolling restart
with no downtime.

This needs more than a rule. EventBridge has **no native "update ECS service"
target** — ECS targets run tasks, they do not update services — so a handler
(most likely a Lambda calling `ecs:UpdateService` with `forceNewDeployment`)
sits between the two. Two further details are unconfirmed: the exact
`detail-type` of the rotation event for an RDS-*managed* master secret, and
whether the rotation schedule on an RDS-owned secret can be managed from
Terraform without fighting RDS over ownership. Because of that uncertainty the
handler is tracked separately and sequenced to start as soon as the cluster
exists, so it lands at or near the first apply.

Stretching the rotation schedule to a long interval as a bridge was considered
and rejected. It is managed rotation in name only, it leaves a scheduled outage
armed behind a config value that someone will one day tidy up, and it provides
no feedback that anything is outstanding. Staging simply absorbs a recoverable
blip if a rotation beats the handler.

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
| 4 | Rotation → forced ECS redeployment              | regional                             | 2          |

Changes 2 and 3 are commits in a single PR and apply together. Applying 2 alone,
with `ebs.tf` and `ec2.tf` still present, would create an EC2 host and a 20 GB
EBS volume on the first apply — which 3 would immediately destroy.

Change 1 must merge and produce an image before the first apply, because
`container_image` is pinned by digest in the HCP workspace variables.

Change 4 is a sibling story under the epic rather than part of this one, and
runs in parallel with 1 and 3.

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

Leave the rotation schedule at its default.

### 4 — Rotation triggers a forced ECS redeployment

An EventBridge rule on the rotation event and a handler that forces a new
deployment. Carries real investigation: the event's `detail-type` for
RDS-managed secrets, whether the schedule is Terraform-manageable against an
RDS-owned secret, whether a Lambda is avoidable in a repository that has none,
and whether the event reliably follows the password change rather than
preceding it.

Blocked by change 2, not change 3 — the research needs a cluster whose secret
can be rotated on demand, not Fargate. Only the final no-downtime verification
needs `desired_count = 2`.

---

## Known Next Steps

Neither is scheduled.

**IAM database authentication.** The application would mint a 15-minute auth
token per connection via `rds.GenerateDBAuthToken` in a pgx `BeforeConnect`
hook, plus `rds-db:connect` in IAM and an `rds_iam` grant on the database role.
No password in the application path at all, and no rotation problem to solve.
Password authentication works correctly with the design above — this would
replace a working mechanism, so it belongs to a deliberate security initiative
rather than this build.

**A database-aware health check.** `/health` is a static 200. A permanent
database failure therefore leaves both tasks in the load balancer serving
errors indefinitely, and a password rotation is invisible to ECS. A check that
fails specifically on Postgres authentication error `28P01` — not on general
database unavailability, which would cascade every task out of service during
an Aurora failover — would make rotation self-healing and close that gap.

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
