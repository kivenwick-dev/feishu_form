#!/bin/zsh
# Resolve the repository from this script's own location so the launcher works
# after cloning to any user's home directory or another folder.
SCRIPT_DIR="${0:A:h}"
export FEISHU_APP_ID="$(security find-generic-password -a "$USER" -s reimbursement-feishu-app-id -w)"
export FEISHU_APP_SECRET="$(security find-generic-password -a "$USER" -s reimbursement-feishu-app-secret -w)"
cd "$SCRIPT_DIR" || exit 1
exec go run ./cmd/feishu-web
