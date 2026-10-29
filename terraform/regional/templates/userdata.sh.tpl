#!/bin/bash
set -euo pipefail
set -x  # trace every command to cloud-init-output.log

exec > >(tee -a /var/log/userdata-debug.log) 2>&1
echo "=== userdata start: $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="

# Register with ECS cluster
echo "=== writing ecs.config ==="
echo "ECS_CLUSTER=${ecs_cluster_name}" >> /etc/ecs/ecs.config
echo "ECS_ENABLE_CONTAINER_METADATA=true" >> /etc/ecs/ecs.config

echo "=== block devices at script start ==="
lsblk -o NAME,TYPE,SIZE,MOUNTPOINT,LABEL || true
ls -la /dev/nvme* /dev/sd* /dev/xvd* 2>/dev/null || true

# Find the data volume device.
# Nitro instances: /dev/sdf attached by Terraform appears as /dev/nvme1n1 (second NVMe disk).
# Xen instances: appears as /dev/sdf or /dev/xvdf.
DATA_DEV=""
for i in $(seq 1 20); do
  echo "=== device scan attempt $i/20 ==="
  lsblk -o NAME,TYPE,SIZE,MOUNTPOINT,LABEL 2>/dev/null || true
  for cand in /dev/nvme1n1 /dev/sdf /dev/xvdf; do
    if [ -b "$cand" ] && [ "$cand" != "/dev/nvme0n1" ]; then
      echo "=== found candidate: $cand ==="
      DATA_DEV="$cand"; break 2
    fi
  done
  sleep 3
done

if [ -z "$DATA_DEV" ]; then
  echo "ERROR: data volume not found after 60s" >&2
  echo "=== final block device state ==="
  lsblk -o NAME,TYPE,SIZE,MOUNTPOINT,LABEL || true
  ls -la /dev/nvme* /dev/sd* /dev/xvd* 2>/dev/null || true
  exit 1
fi

echo "=== using device: $DATA_DEV ==="
blkid "$DATA_DEV" || true

# Format only if no filesystem exists (idempotent across reboots)
if ! blkid "$DATA_DEV" > /dev/null 2>&1; then
  echo "=== no filesystem detected, formatting $DATA_DEV ==="
  mkfs.ext4 -L ecs-data "$DATA_DEV"
else
  echo "=== filesystem already exists on $DATA_DEV, skipping format ==="
fi

echo "=== mounting $DATA_DEV at /mnt/ecs-data ==="
mkdir -p /mnt/ecs-data
mount "$DATA_DEV" /mnt/ecs-data
echo "LABEL=ecs-data /mnt/ecs-data ext4 defaults,nofail 0 2" >> /etc/fstab

echo "=== setting permissions ==="
# Container runs as UID 1001 (USER 1001 in Containerfile)
chmod 777 /mnt/ecs-data

echo "=== userdata complete: $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
