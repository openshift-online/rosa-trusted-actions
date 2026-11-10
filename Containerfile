FROM registry.access.redhat.com/hi/go:latest AS builder

WORKDIR /opt/app-root/src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o bin/rosa-trusted-actions-server ./cmd/server

FROM registry.access.redhat.com/hi/core-runtime:latest@sha256:81280d7fd6afa7a3d2a6da2e90e545cf8e5d187c80a69b2a1c875fdcd731aaed

COPY --from=builder /opt/app-root/src/bin/rosa-trusted-actions-server /usr/local/bin/

EXPOSE 8080

USER 1001

ENTRYPOINT ["rosa-trusted-actions-server"]
