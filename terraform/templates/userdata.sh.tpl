#!/bin/bash
set -euo pipefail

# Register with ECS cluster
echo "ECS_CLUSTER=${ecs_cluster_name}" >> /etc/ecs/ecs.config
echo "ECS_ENABLE_CONTAINER_METADATA=true" >> /etc/ecs/ecs.config

# Find the data volume device.
# Nitro instances: /dev/sdf attached by Terraform appears as /dev/nvme1n1 (second NVMe disk).
# Xen instances: appears as /dev/sdf or /dev/xvdf.
DATA_DEV=""
for i in $(seq 1 20); do
  for cand in /dev/nvme1n1 /dev/sdf /dev/xvdf; do
    if [ -b "$cand" ] && [ "$cand" != "/dev/nvme0n1" ]; then
      DATA_DEV="$cand"; break 2
    fi
  done
  sleep 3
done

if [ -z "$DATA_DEV" ]; then
  echo "ERROR: data volume not found after 60s" >&2; exit 1
fi

# Format only if no filesystem exists (idempotent across reboots)
if ! blkid "$DATA_DEV" > /dev/null 2>&1; then
  mkfs.ext4 -L ecs-data "$DATA_DEV"
fi

mkdir -p /mnt/ecs-data
mount "$DATA_DEV" /mnt/ecs-data
echo "LABEL=ecs-data /mnt/ecs-data ext4 defaults,nofail 0 2" >> /etc/fstab

# Container runs as UID 1001 (USER 1001 in Containerfile)
chmod 777 /mnt/ecs-data
