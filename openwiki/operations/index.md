# Files

- [Operations: Configuration Reference](configuration.md) - Reference for every CLI flag, environment variable, and config-file key the ROSA Trusted Actions server reads, including precedence rules and the fail-fast guards enforced at startup.
- [Operations: Deployment Architecture](deployment.md) - How the service is built and deployed to AWS across regions — the two-root Terraform split, HCP Terraform workspace apply order, container build and CI/CD pipelines, and the private multi-region networking/TLS model.
- [Operations: Local Development Environment](local-development.md) - How to build, run, lint, and test the ROSA Trusted Actions server locally, including the mock-auth path, local Postgres via compose, and the helper scripts that scaffold staging credentials and smoke-test the API.
