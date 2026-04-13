FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pre ./cmd/pre

FROM node:20-slim
COPY --from=builder /pre /usr/local/bin/pre

ENV HOME=/root
ENV SHELL=/bin/bash

RUN pre setup

WORKDIR /demo
CMD ["bash"]
