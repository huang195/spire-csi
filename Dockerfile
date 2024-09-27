FROM golang:1-alpine as builder
RUN mkdir /build
ADD . /build/
WORKDIR /build
RUN go build -o spiffe-csi-driver ./cmd/spiffe-csi-driver

FROM alpine:latest
COPY --from=builder /build/spiffe-csi-driver /bin/spiffe-csi-driver
COPY --from=ghcr.io/spiffe/spire-agent:1.5.1 /opt/spire/bin/spire-agent /bin/spire-agent

ENTRYPOINT ["/bin/spiffe-csi-driver"]
