FROM registry.access.redhat.com/hi/go:latest AS builder

WORKDIR /opt/app-root/src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o bin/rosa-trusted-actions-server ./cmd/server

FROM registry.access.redhat.com/hi/core-runtime@sha256:4163796d8b7f28021178093d31cb97c05336fd3f3a4d7006603a7fef29e997e7

COPY --from=builder /opt/app-root/src/bin/rosa-trusted-actions-server /usr/local/bin/

EXPOSE 8080

USER 1001

ENTRYPOINT ["rosa-trusted-actions-server"]
