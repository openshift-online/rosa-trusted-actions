# describe-nodes

The `describe-nodes` action provides structured data equivalent to `oc describe node`. 

## What it returns

One object per node containing:

- Node metadata (labels, annotations, creation timestamp)
- Node spec (taints, unschedulable, providerID, podCIDR)
- Node status (conditions, addresses, capacity, allocatable, systemInfo)
- Non-terminated pods on the node (name, namespace, per-container resources)
- Node events (type, reason, message, source, timestamps, count)
- Lease data (holder identity, acquire/renew times)
