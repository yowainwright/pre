FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pre ./cmd/pre

FROM node:20-slim

RUN useradd -m -s /bin/bash demo

COPY --from=builder /pre /usr/local/bin/pre
COPY opts/demo/ /home/demo/demo/

RUN chown -R demo:demo /home/demo/demo

USER demo
ENV HOME=/home/demo
WORKDIR /home/demo/demo

RUN pre setup

CMD ["bash", "-i"]
