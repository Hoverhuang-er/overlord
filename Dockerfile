FROM apache/skywalking-go:0.3.0-go1.20 as builder
ARG DIR
ARG TARGETARCH
ARG TARGETOS
ENV DIR=${DIR}
ENV TARGETARCH=${TARGETARCH}
ENV TARGETOS=${TARGETOS}
RUN echo "DIR: ${DIR}"
WORKDIR /opt
COPY . .
RUN chmod a+x /usr/local/bin/skywalking-go-agent
RUN ls -lath
RUN go mod tidy && \
    pwd && \
    /usr/local/bin/skywalking-go-agent -inject $(pwd) && \
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -toolexec="/usr/local/bin/skywalking-go-agent -config /opt/scripts/agent.default.yaml" -a -o /opt/app ./cmd/${DIR}/main.go
FROM debian:trixie-slim as final
ARG DIR
ENV DIR=${DIR}
WORKDIR /opt
COPY --from=builder /opt/app /opt/app
COPY --from=builder /opt/cmd/${DIR}/*.toml .
ENTRYPOINT ["/opt/app"]