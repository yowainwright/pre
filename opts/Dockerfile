FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pre ./cmd/pre

FROM node:24-slim

RUN apt-get update && apt-get install -y python3-pip --no-install-recommends && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash demo

COPY --from=builder /pre /usr/local/bin/pre
COPY opts/demo/ /home/demo/demo/

RUN echo '{"name":"pre-demo","version":"1.0.0","private":true,"dependencies":{"lodash":"^4.17.21"}}' \
    > /home/demo/demo/package.json

RUN chown -R demo:demo /home/demo/demo

USER demo
ENV HOME=/home/demo
WORKDIR /home/demo/demo

RUN pre setup

CMD ["bash", "/home/demo/demo/demo.sh"]
