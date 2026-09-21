FROM golang:1.26.8-bookworm AS patcher
COPY testing/officialets/patches/wfs202-locking.go /patch.go
RUN CGO_ENABLED=0 go build -o /patch /patch.go

FROM ogccite/ets-wfs20:1.42-teamengine-5.7@sha256:d8099237033172d23ba1beefbb16f6bd46025354a3f57c74ae009d9b91d7525b
COPY --from=patcher /patch /tmp/patch
RUN /tmp/patch /usr/local/tomcat/webapps/teamengine/WEB-INF/lib/ets-wfs20-1.42.jar && rm /tmp/patch
LABEL org.neoserver.ets-evidence="official-derived" \
      org.neoserver.ets-patch="wfs202-locking-v2"
