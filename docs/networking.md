# Private Networking & DNS Architecture

This document explains how the ROSA Trusted Actions Server exposes a stable,
VPC-internal FQDN across multiple regional deployments without any public DNS
entry or publicly reachable endpoint, and how TLS certificates are issued for
that private name.

---

## Table of Contents

1. [Overview](#overview)
2. [Multi-Region Architecture](#multi-region-architecture)
3. [DNS Resolution Inside a VPC](#dns-resolution-inside-a-vpc)
4. [Cross-Region Isolation](#cross-region-isolation)
5. [TLS Certificates — The Shadow Zone Pattern](#tls-certificates--the-shadow-zone-pattern)
6. [Multi-Region Deployment Order](#multi-region-deployment-order)
7. [Terraform Variable Reference](#terraform-variable-reference)

---

## Overview

The service is deployed once per AWS region. Each regional deployment is
completely self-contained: it has its own VPC, its own internal Application
Load Balancer (ALB), and its own Route 53 Private Hosted Zone (PHZ).

Every deployment uses the **same FQDN** — for example
`rosa-trusted-actions.internal.company.com` — but that name resolves to a
**different ALB** depending on which VPC the caller is in. Traffic never
crosses a regional boundary at the DNS layer.

This is the same model Kubernetes uses for internal service URLs
(`my-service.my-namespace.svc.cluster.local`), implemented entirely with AWS
primitives.

---

## Multi-Region Architecture

```mermaid
flowchart TB
    subgraph public-dns ["Public Route 53 (global)"]
        PZ["Hosted Zone\ninternal.company.com\n\n_acme-challenge CNAME only\n(ACM validation record)\n\nNo A record — NXDOMAIN\nfor the service FQDN"]
    end

    subgraph us-east-1 ["AWS us-east-1"]
        subgraph vpc1 ["VPC vpc-us-east-1"]
            PHZ1["Private Hosted Zone\nrosa-trusted-actions.internal.company.com\nZone ID: Z1AAA\n\nA → internal-alb-1"]
            ALB1["Internal ALB\ninternal-alb-1.us-east-1.elb.amazonaws.com"]
            ECS1["ECS Cluster\nrosa-trusted-actions"]
        end
    end

    subgraph eu-west-1 ["AWS eu-west-1"]
        subgraph vpc2 ["VPC vpc-eu-west-1"]
            PHZ2["Private Hosted Zone\nrosa-trusted-actions.internal.company.com\nZone ID: Z2BBB\n\nA → internal-alb-2"]
            ALB2["Internal ALB\ninternal-alb-2.eu-west-1.elb.amazonaws.com"]
            ECS2["ECS Cluster\nrosa-trusted-actions"]
        end
    end

    subgraph ap-southeast-1 ["AWS ap-southeast-1"]
        subgraph vpc3 ["VPC vpc-ap-southeast-1"]
            PHZ3["Private Hosted Zone\nrosa-trusted-actions.internal.company.com\nZone ID: Z3CCC\n\nA → internal-alb-3"]
            ALB3["Internal ALB\ninternal-alb-3.ap-southeast-1.elb.amazonaws.com"]
            ECS3["ECS Cluster\nrosa-trusted-actions"]
        end
    end

    PHZ1 --> ALB1 --> ECS1
    PHZ2 --> ALB2 --> ECS2
    PHZ3 --> ALB3 --> ECS3

    PZ -. "ACM validates cert\nfor each deployment\n(DNS-01 CNAME only)" .-> PHZ1
    PZ -. "ACM validates cert\nfor each deployment\n(DNS-01 CNAME only)" .-> PHZ2
    PZ -. "ACM validates cert\nfor each deployment\n(DNS-01 CNAME only)" .-> PHZ3
```

**Key points:**

- The three Private Hosted Zones have identical names but different Zone IDs.
  AWS Route 53 identifies zones by ID, not by name, so there is no conflict.
- Each PHZ is associated exclusively with one VPC. Route 53 Resolver inside
  that VPC only sees its own zone.
- The public hosted zone contains **no A record** for the service FQDN — only
  the ACM validation CNAME (explained below). The name is publicly
  unresolvable (NXDOMAIN).

---

## DNS Resolution Inside a VPC

When any workload inside a VPC queries `rosa-trusted-actions.internal.company.com`,
AWS routes the query through the VPC's built-in Route 53 Resolver (always
available at the VPC base CIDR + 2, e.g. `10.0.0.2`). The resolver looks up
all Private Hosted Zones associated with the querying VPC and answers from the
matching zone — entirely within the VPC, with no public DNS involved.

```mermaid
sequenceDiagram
    participant C  as Caller<br/>(ECS task / Lambda / EC2)
    participant R  as Route 53 Resolver<br/>(VPC DNS, :53)
    participant PZ as Private Hosted Zone<br/>rosa-trusted-actions.internal.company.com<br/>(associated to this VPC only)
    participant AL as Internal ALB<br/>(private subnets)
    participant SV as ECS Service<br/>(port 8080)

    C->>R: DNS query<br/>rosa-trusted-actions.internal.company.com
    Note over R: Checks zones associated<br/>with this VPC only
    R->>PZ: Look up A record
    PZ-->>R: ALIAS → internal-alb.region.elb.amazonaws.com<br/>(resolved to ALB private IP)
    R-->>C: 10.0.x.x (ALB private IP)
    C->>AL: HTTPS :443 → 10.0.x.x
    AL->>SV: HTTP :8080 (target group)
    SV-->>AL: 200 OK
    AL-->>C: 200 OK
```

**What makes this VPC-local:**

- `enable_dns_support = true` on the VPC tells EC2 instances and containers
  to use the Route 53 Resolver at VPC+2 instead of a custom DNS server.
- `enable_dns_hostnames = true` allows ALB DNS names to resolve within the VPC.
- The PHZ association scope is a single VPC. Queries from outside that VPC
  receive no answer from this zone — the zone is invisible to them.

---

## Cross-Region Isolation

The same FQDN exists in three separate Private Hosted Zones. Because each zone
is associated with exactly one VPC, a caller in `us-east-1` cannot resolve the
`eu-west-1` zone even if both zones have the same name.

```mermaid
flowchart LR
    subgraph vpc-us ["VPC us-east-1"]
        C1["Caller"] -->|"query: rosa-trusted-actions\n.internal.company.com"| R1["Resolver\n10.0.0.2"]
        R1 -->|"associated zone:\nZ1AAA"| A1["→ ALB us-east-1\n10.0.x.x"]
    end

    subgraph vpc-eu ["VPC eu-west-1"]
        C2["Caller"] -->|"query: rosa-trusted-actions\n.internal.company.com"| R2["Resolver\n10.1.0.2"]
        R2 -->|"associated zone:\nZ2BBB"| A2["→ ALB eu-west-1\n10.1.x.x"]
    end

    Z1["PHZ Z1AAA\n(us-east-1 only)"]
    Z2["PHZ Z2BBB\n(eu-west-1 only)"]

    R1 -.->|"sees"| Z1
    R1 -.->|"cannot see"| Z2
    R2 -.->|"sees"| Z2
    R2 -.->|"cannot see"| Z1
```

There is no routing policy, no latency-based record, and no failover. The
isolation is enforced structurally by the PHZ association scope — not by
configuration that could be misconfigured.

---

## TLS Certificates — The Shadow Zone Pattern

Let's Encrypt and ACM public certificates both support a **DNS-01** validation
challenge: instead of serving a file over HTTP, you prove domain ownership by
placing a specific CNAME record in your DNS zone. Critically, the validation
server only needs to **resolve** that CNAME — it never makes an HTTP request to
the service itself.

This means a certificate can be issued for a name that has no public A record,
as long as the DNS-01 CNAME is in a publicly resolvable zone. The service
endpoint can remain entirely private.

```mermaid
flowchart TB
    subgraph public-zone ["Public Route 53 Zone — internal.company.com"]
        direction TB
        CNAME["_acme-challenge.rosa-trusted-actions\n.internal.company.com\n→ (ACM validation token)\n\nType: CNAME — written by Terraform"]
        NORECORD["rosa-trusted-actions.internal.company.com\n\n(no record exists here → NXDOMAIN\nfrom the public internet)"]
    end

    subgraph private-zone ["Private Hosted Zone — rosa-trusted-actions.internal.company.com"]
        AREC["rosa-trusted-actions.internal.company.com\n→ internal-alb.region.elb.amazonaws.com\n\nType: A alias — only visible inside the VPC"]
    end

    ACM["AWS ACM\n(certificate authority)"]
    CERT["ACM Certificate\nrosa-trusted-actions.internal.company.com\n\nIssued by Amazon Trust Services\nTrusted by all standard clients"]

    ACM -->|"1. places validation CNAME\nin public zone"| CNAME
    ACM -->|"2. resolves CNAME\nfrom public internet"| CNAME
    CNAME -->|"3. validation succeeds"| ACM
    ACM -->|"4. issues certificate"| CERT
    CERT -->|"5. attached to\ninternal ALB HTTPS listener"| AREC

    style NORECORD fill:#fee,stroke:#c00
    style CNAME fill:#efe,stroke:#080
    style AREC fill:#eef,stroke:#008
```

**Why this is safe:**

| What the public internet can see | What it cannot reach |
|---|---|
| `_acme-challenge.rosa-trusted-actions...` CNAME (ACM validation) | Any A record for the service |
| Certificate in Certificate Transparency logs | The service itself |
| That a certificate was issued | Any response from the ALB |

The certificate is issued by Amazon Trust Services and is trusted by all
standard TLS clients without any custom CA distribution — unlike a fully
private CA approach (AWS Private CA), which would require distributing a root
certificate to every caller.

**ACM auto-renewal** works identically: ACM re-places the CNAME, re-validates
through the public zone, and rotates the certificate — no manual intervention,
no service disruption.

---

## Multi-Region Deployment Order

Every regional deployment issues its own ACM certificate (ACM is a regional
service; an ALB in `eu-west-1` cannot use a certificate from `us-east-1`).
Because all certs cover the same FQDN, ACM computes an **identical** DNS-01
validation CNAME for each of them. That CNAME is a single record in the shared
public Route 53 zone — it is global infrastructure, not per-region.

This creates a hard constraint: the validation CNAME must be owned by exactly
one Terraform workspace. If every workspace tried to create and destroy the
same record independently, parallel applies would race on shared state and
parallel destroys would delete a record that surviving regions still need for
certificate auto-renewal.

### The `manage_validation_record` variable

The variable `manage_validation_record` (type `bool`, default `false`) controls
which workspace owns the CNAME record.

- **Exactly one region** sets `manage_validation_record = true`. This workspace
  creates the `aws_route53_record.cert_validation` resource and owns it in
  Terraform state. By convention this is the **primary region** (the one that
  must be applied first).
- **All other regions** leave `manage_validation_record = false`. They create
  their own ACM certificate and then wait for ACM to report it as `ISSUED`,
  which happens automatically once the CNAME placed by the primary region is
  in place. They never touch the shared Route 53 record and hold no Terraform
  dependency on it, so they can apply and destroy fully in parallel.

```mermaid
flowchart TD
    subgraph step1 ["Step 1 — apply primary region first"]
        A1["us-east-1\nmanage_validation_record = true"]
        A1 --> CNAME["Route 53 public zone\n_acme-challenge CNAME written\n(owned by us-east-1 workspace)"]
        A1 --> CERT1["ACM cert us-east-1\nstatus: ISSUED"]
    end

    subgraph step2 ["Step 2 — apply remaining regions in parallel"]
        A2["eu-west-1\nmanage_validation_record = false"]
        A3["ap-southeast-1\nmanage_validation_record = false"]
        A2 --> CERT2["ACM cert eu-west-1\nwaits → ISSUED"]
        A3 --> CERT3["ACM cert ap-southeast-1\nwaits → ISSUED"]
    end

    CNAME -->|"ACM resolves CNAME\n(validates all regions)"| CERT2
    CNAME -->|"ACM resolves CNAME\n(validates all regions)"| CERT3
```

### Workspace variable summary

```
# us-east-1 — primary, apply first
internal_fqdn             = "rosa-trusted-actions.internal.company.com"
public_zone_id            = "Z0123456789ABC"
manage_validation_record  = true

# eu-west-1 — secondary, apply after us-east-1
internal_fqdn             = "rosa-trusted-actions.internal.company.com"
public_zone_id            = "Z0123456789ABC"
manage_validation_record  = false   # ← default; CNAME already exists

# ap-southeast-1 — secondary, apply after us-east-1
internal_fqdn             = "rosa-trusted-actions.internal.company.com"
public_zone_id            = "Z0123456789ABC"
manage_validation_record  = false   # ← default; CNAME already exists
```

### Destruction order

Destruction is the mirror image of deployment:

1. **Destroy secondary regions first** (in parallel, `manage_validation_record
   = false`). Their workspaces do not own the CNAME, so destroying them leaves
   the record intact.
2. **Destroy the primary region last** (`manage_validation_record = true`).
   This workspace deletes the CNAME as the final step, at which point no other
   regional deployment remains that could need it for certificate renewal.

If the primary region is destroyed while secondary regions are still running,
the validation CNAME disappears and ACM will be unable to auto-renew the
surviving certificates when they approach expiry.

### Transferring ownership

If the primary region must be decommissioned while other regions remain live,
transfer ownership before destroying it:

1. Set `manage_validation_record = true` in a different region and apply that
   workspace — the record already exists so Terraform will adopt it into the
   new workspace's state.
2. Set `manage_validation_record = false` in the old primary and apply — the
   record is removed from that workspace's state but **not deleted from Route
   53** (because another workspace now owns it).
3. Destroy the old primary region normally.

---

## Terraform Variable Reference

| Variable | Default | Purpose |
|---|---|---|
| `internal_fqdn` | `""` | Full private FQDN, e.g. `rosa-trusted-actions.internal.company.com`. Creates the PHZ and (with `public_zone_id`) the ACM certificate. |
| `public_zone_id` | `""` | Route 53 hosted zone ID for the **public** apex zone. Only the ACM DNS-01 CNAME is written here — no A record. |
| `manage_validation_record` | `false` | Set `true` in exactly one regional deployment (the primary). That workspace owns and manages the shared ACM validation CNAME. All other regions must be `false`. |
| `alb_certificate_arn` | `""` | Bypass for manual certificate management. Set instead of `public_zone_id` when the apex zone is not in Route 53. |
