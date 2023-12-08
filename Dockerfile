FROM golang:1.21.5-alpine3.18 as builder
ARG DIR
ARG TARGETARCH
ARG TARGETOS
WORKDIR /opt
COPY . .
RUN ls -lath
RUN go mod tidy
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /opt/${DIR} ./cmd/${DIR}
FROM debian:trixie-slim as final
WORKDIR /opt
COPY --from=builder /opt/${DIR} /opt/${DIR}
COPY --from=builder /opt/cmd/${DIR}/* ./
ENTRYPOINT ["/opt/${DIR}"]