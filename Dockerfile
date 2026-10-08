FROM golang:1.27.1-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates git file musl-tools gcc-11 && rm -rf /var/lib/apt/lists/*
WORKDIR /code
COPY . .
ARG COMMIT
ARG VERSION=12.0.0-rc.1
RUN COMMIT="$COMMIT" VERSION="$VERSION" REALGCC=gcc-11 bash scripts/build_v12_release.sh

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl bash jq && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 1025 dungeon && useradd -m -u 1025 -g dungeon dungeon
COPY --from=builder /code/build/v12-release/dungeond /usr/bin/dungeond
USER 1025:1025
WORKDIR /home/dungeon
EXPOSE 1317 26656 26657 9090
CMD ["dungeond", "version"]
