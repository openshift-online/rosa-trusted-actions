FROM registry.access.redhat.com/hi/go:latest AS builder

WORKDIR /opt/app-root/src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o bin/rosa-trusted-actions-server ./cmd/server

FROM registry.access.redhat.com/hi/core-runtime:latest

COPY --from=builder /opt/app-root/src/bin/rosa-trusted-actions-server /usr/local/bin/

EXPOSE 8080

USER 1001

ENTRYPOINT ["rosa-trusted-actions-server"]
