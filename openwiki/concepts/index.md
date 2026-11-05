# Files

- [Authorization Model](authorization-model.md) - How ROSA Trusted Actions layers JWT authentication, AMS role resolution, per-action role catalogs, and per-scope Kubernetes/AWS resource authorizers to decide whether a request may run.
- [Data Model & Persistence](data-model-and-persistence.md) - Explains the Execution, AuditEntry, and ExecutionOutput data models, the backend-agnostic Store interface, and the dual Postgres/SQLite persistence layer including migrations and shared query-building helpers.
- [Execution Pipeline & Scopes](execution-pipeline.md) - How internal/executor.Executor and its Scope implementations (KubeScope, AWSScope) authorize and run a trusted action independent of HTTP, and how the same pipeline is shared by the async worker and the local action-cli.
- [Trusted Actions & Catalog](trusted-actions-catalog.md) - Defines the Trusted Action abstraction (Action/KubeAction/AWSAction/AWSWriteAction), its self-registering implementation registry, and the catalog that declares each action's policy.
