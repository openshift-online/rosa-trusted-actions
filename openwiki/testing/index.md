# Files

- [Testing: Integration Harnesses & Mocks](integration-and-mocks.md) - How rosa-trusted-actions tests the full HTTP-to-cluster-to-AWS path locally and in CI, using a disposable kind+LocalStack environment and two interchangeable Backplane test doubles (an Imposter fixture mock and a real reverse proxy).
- [Testing: Unit Test Conventions & Coverage](testing-strategy.md) - Documents the project's unit testing conventions, Make-based test targets, the >=80% patch coverage policy enforced by Codecov, and the shared store test suite pattern used to validate both database backends against one contract.
