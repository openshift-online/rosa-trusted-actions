FROM registry.access.redhat.com/hi/go:latest AS builder

WORKDIR /opt/app-root/src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o bin/rosa-trusted-actions-server ./cmd/server

FROM registry.access.redhat.com/hi/core-runtime:latest

COPY --from=builder /opt/app-root/src/bin/rosa-trusted-actions-server /usr/local/bin/

# AWS rotates this CA bundle on a published schedule; fetch at build time
# rather than committing it, so a rebuild picks up renewals.
ADD https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem /etc/ssl/certs/rds-global-bundle.pem
RUN chmod 0644 /etc/ssl/certs/rds-global-bundle.pem

EXPOSE 8080

USER 1001

ENTRYPOINT ["rosa-trusted-actions-server"]
