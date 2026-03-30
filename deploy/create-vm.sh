#!/bin/bash
# Create a free-tier GCP e2-micro VM for guest-stay.
# Prerequisites: gcloud CLI installed and authenticated.
#
# Usage: bash deploy/create-vm.sh

set -euo pipefail

PROJECT="${GCP_PROJECT:?Set GCP_PROJECT env var (e.g. export GCP_PROJECT=my-project-id)}"
VM_NAME="guest-stay"
ZONE="us-central1-a"   # free-tier eligible
MACHINE_TYPE="e2-micro" # free-tier eligible

echo "==> Creating VM in project: ${PROJECT}"

# Create the VM
gcloud compute instances create "$VM_NAME" \
    --project="$PROJECT" \
    --zone="$ZONE" \
    --machine-type="$MACHINE_TYPE" \
    --image-family=debian-12 \
    --image-project=debian-cloud \
    --boot-disk-size=30GB \
    --boot-disk-type=pd-standard \
    --tags=http-server,https-server

# Create firewall rules if they don't exist
gcloud compute firewall-rules describe allow-http --project="$PROJECT" &>/dev/null 2>&1 || \
    gcloud compute firewall-rules create allow-http \
        --project="$PROJECT" \
        --allow=tcp:80 \
        --target-tags=http-server \
        --description="Allow HTTP"

gcloud compute firewall-rules describe allow-https --project="$PROJECT" &>/dev/null 2>&1 || \
    gcloud compute firewall-rules create allow-https \
        --project="$PROJECT" \
        --allow=tcp:443 \
        --target-tags=https-server \
        --description="Allow HTTPS"

# Reserve a static IP
gcloud compute addresses create guest-stay-ip \
    --project="$PROJECT" \
    --region=us-central1 2>/dev/null || true

STATIC_IP=$(gcloud compute addresses describe guest-stay-ip \
    --project="$PROJECT" \
    --region=us-central1 \
    --format="get(address)")

# Assign static IP to VM
gcloud compute instances delete-access-config "$VM_NAME" \
    --project="$PROJECT" \
    --zone="$ZONE" \
    --access-config-name="external-nat" 2>/dev/null || true

gcloud compute instances add-access-config "$VM_NAME" \
    --project="$PROJECT" \
    --zone="$ZONE" \
    --address="$STATIC_IP"

echo ""
echo "==> VM created!"
echo ""
echo "  Static IP: ${STATIC_IP}"
echo "  SSH:       gcloud compute ssh ${VM_NAME} --zone=${ZONE} --project=${PROJECT}"
echo ""
echo "Next steps:"
echo "  1. Point DNS: guest-stay.jesco39.com -> ${STATIC_IP} (A record)"
echo "  2. SSH into the VM and run:  sudo bash /opt/guest-stay/deploy/setup-vm.sh"
echo "  3. Create /opt/guest-stay/.env with production values"
echo "  4. From your local machine:  ./deploy.sh ${STATIC_IP}"
