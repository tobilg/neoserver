FROM alpine:3.21
RUN apk add --no-cache bash curl jq
COPY scripts/test-setup/setup-demo-workspace.sh /usr/local/bin/setup-demo-workspace
ENTRYPOINT ["/bin/bash", "/usr/local/bin/setup-demo-workspace"]
