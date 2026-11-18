# SPDX-License-Identifier: AGPL-3.0-only
# SwarmAiNet-Knoten als Container, vor allem für Helferknoten und Tracker
# (spec/p2p-layer.md § 6.1). Bauen: docker build -t swarmainet .
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/swarmainet ./cmd/swarmainet && mkdir /out/data

# Statisches Binary, kein Shell, läuft als Benutzer nonroot (UID 65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/swarmainet /usr/local/bin/swarmainet
# /data gehört nonroot, damit auch benannte Volumes beschreibbar sind.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 4711/tcp 4711/udp
LABEL org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/rainer-sharp/Ai-Swarms.net"
ENTRYPOINT ["/usr/local/bin/swarmainet"]
CMD ["run", "-config", "/data/config.yaml"]
