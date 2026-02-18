FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

# Copy everything (filtered by .dockerignore)
COPY go.work go.work.sum ./
COPY core/ ./core/
COPY impl/ ./impl/
COPY cmd/ ./cmd/

RUN go work sync && go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /routes-provisioner ./cmd/routes-provisioner

# --- Final image ---
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /routes-provisioner /routes-provisioner

ENTRYPOINT ["/routes-provisioner"]
CMD ["--config=/etc/routes-provisioner/config.yaml"]
