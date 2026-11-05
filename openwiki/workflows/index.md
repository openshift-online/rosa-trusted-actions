# Files

- [Workflow: Adding a New Trusted Action](adding-a-trusted-action.md) - Canonical contributor workflow for authoring, registering, testing, reviewing, and shipping a new Trusted Action, covering both the Kube-action and AWS-action paths.
- [Workflow: CLI Clients (rta, action-cli, test-client)](cli-clients.md) - Explains the three command-line entry points in the repository — rta (the production HTTP API client), action-cli (a direct-executor developer CLI), and test-client (a thin HTTP smoke-test tool) — and when to use each.
- [Workflow: Executing a Trusted Action](executing-a-trusted-action.md) - End-to-end lifecycle of a single trusted-action execution — from POST /{action}/run through asynchronous worker processing to GET /runs/{id} polling, covering the client-facing contract, failure classification, and graceful shutdown.
