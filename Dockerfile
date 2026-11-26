# SPDX-License-Identifier: AGPL-3.0-only
# AI-Swarms.net-Knoten als Container, vor allem für Helferknoten und Tracker
# (spec/p2p-layer.md § 6.1). Bauen: docker build -t swarmainet .
# Mit Version (sonst meldet der Knoten „dev“, .git kommt nicht ins Image):
#   docker build --build-arg VERSION=$(git describe --tags --always --dirty) -t swarmainet .
# Mit ffmpeg, für ein Gateway, das lange Aufnahmen in Stücke teilt
# (docs/usage.md, Transkription): docker build --target audio -t swarmainet:audio .
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=""
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/swarmainet ./cmd/swarmainet && mkdir /out/data

# Variante mit ffmpeg/ffprobe (Alpine, deutlich kleiner als Debian), sonst
# wie das Standard-Image.
FROM alpine:3.21 AS audio
RUN apk add --no-cache ffmpeg \
    && adduser -S -D -H -u 65532 -h /data nonroot
COPY --from=build /out/swarmainet /usr/local/bin/swarmainet
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
VOLUME /data
EXPOSE 4711/tcp 4711/udp
LABEL org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/rainer-sharp/Ai-Swarms.net"
ENTRYPOINT ["/usr/local/bin/swarmainet"]
CMD ["run", "-config", "/data/config.yaml"]

# Standard (letzte Stufe): statisches Binary, kein Shell, läuft als Benutzer nonroot (UID 65532).
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
