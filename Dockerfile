FROM golang:1.26-alpine AS builder

WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go run ./cmd/build -targets ${TARGETOS}/${TARGETARCH} -out /tmp/dist \
	&& cp /tmp/dist/${TARGETOS}-${TARGETARCH}/feishu-web /tmp/feishu-web

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
	&& adduser -D -H -u 10001 app \
	&& mkdir -p /app /data/outputs \
	&& chown -R app:app /app /data

COPY --from=builder /tmp/feishu-web /app/feishu-web

ENV NO_BROWSER_OPEN=1 \
	REIMBURSEMENT_HOST=0.0.0.0 \
	REIMBURSEMENT_PORT=8765 \
	REIMBURSEMENT_OUTPUT_DIR=/data/outputs \
	REIMBURSEMENT_DISABLE_FOLDER_PICKER=1 \
	TZ=Asia/Shanghai

WORKDIR /app
USER app
EXPOSE 8765

ENTRYPOINT ["/app/feishu-web"]
