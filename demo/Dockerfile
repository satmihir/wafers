FROM golang:1.26-bookworm

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    fuse-overlayfs \
    fuse3 \
    git \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

CMD ["bash", "demo/run.sh"]
