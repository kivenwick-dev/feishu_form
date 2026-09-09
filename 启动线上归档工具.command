#!/bin/zsh
PROJECT_DIR="/Users/john/Documents/My_Work/报销表拆分工具"
export FEISHU_APP_ID="$(security find-generic-password -a "$USER" -s reimbursement-feishu-app-id -w)"
export FEISHU_APP_SECRET="$(security find-generic-password -a "$USER" -s reimbursement-feishu-app-secret -w)"
cd "$PROJECT_DIR" || exit 1
exec go run ./cmd/feishu-web
