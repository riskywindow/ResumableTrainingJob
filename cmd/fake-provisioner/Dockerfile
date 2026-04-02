FROM golang:1.25-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o fake-provisioner ./cmd/fake-provisioner

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/fake-provisioner .
USER 65532:65532
ENTRYPOINT ["/fake-provisioner"]
