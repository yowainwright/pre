FROM golang:1.26.5-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pre ./cmd/pre

FROM node:24-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash tester
RUN python3 -m venv /home/tester/.venv

COPY --from=builder /pre /usr/local/bin/pre
COPY tests/e2e/*_test.sh /home/tester/e2e/

RUN chmod +x /home/tester/e2e/*.sh \
    && mkdir -p /home/tester/workspace \
    && chown -R tester:tester /home/tester

USER tester
ENV HOME=/home/tester
ENV PATH=/home/tester/.venv/bin:$PATH
WORKDIR /home/tester/workspace

RUN pre setup

ENTRYPOINT ["/home/tester/e2e/package_manager_test.sh"]
CMD ["npm"]
