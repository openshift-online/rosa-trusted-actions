# Files

- [API Surface & Code Generation](api-surface.md) - How the Trusted Actions HTTP API is designed OpenAPI-first — the openapi/ spec layout, the bundle/generate pipeline that produces internal/openapi/api.go, and the boundary between generated code and the hand-written handlers that implement it.
- [System Architecture Overview](overview.md) - Big-picture shape of the ROSA Trusted Actions server — process wiring in cmd/server/main.go, the chi middleware stack, and how a request flows from the HTTP API through authn/authz, the worker pool, and out to a cluster or AWS.
