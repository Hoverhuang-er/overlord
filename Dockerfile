FROM golang:1.21.5-alpine3.18 as builder
ARG DIR
ARG TARGETARCH
ARG TARGETOS
ENV DIR=${DIR}
ENV TARGETARCH=${TARGETARCH}
ENV TARGETOS=${TARGETOS}
WORKDIR /opt
COPY . .
RUN ls -lath
RUN go mod tidy
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /opt/${DIR} ./cmd/${DIR}/main.go
FROM debian:trixie-slim as final
ARG DIR
ENV DIR=${DIR}
WORKDIR /opt
COPY --from=builder /opt/${DIR} /opt/${DIR}
COPY --from=builder /opt/cmd/${DIR}/* ./
ENTRYPOINT ["/opt/${DIR}"]