# Files

- [Integration: AWS Access & Actions](aws.md) - How the server obtains AWS credentials per target cluster through awsapi.ClientProvider, and how AWS actions declare least-privilege clients and have their reboot/terminate mutations gated by authorization.AWSAuthorizer.
- [Integration: Backplane (Cluster Access)](backplane.md) - How the server reaches a managed cluster's Kubernetes API through RBAC-scoped backplane sessions (BackplaneProvider) or a local kubeconfig (KubeconfigProvider), and the two local test doubles used to exercise this without a real backplane.
- [Integration: OCM (Authentication & Authorization)](ocm.md) - How the server validates OCM-issued JWTs via a JWKS wrapper, resolves caller identity and AMS roles through internal/ocm, and looks up clusters by ID, external ID, or display name.
