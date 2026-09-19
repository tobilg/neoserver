FROM ogccite/ets-wcs20:1.21-teamengine-5.7@sha256:07aa1ce786e89bd00bef63a270b63c962b6f60b854712f112bc660a31f5a5f9e

COPY testing/officialets/patches/wcs20-interpolation-entrypoint.sh /opt/neoserver/wcs20-interpolation-entrypoint.sh

LABEL org.neoserver.ets-evidence="official-derived" \
      org.neoserver.ets-patch="wcs20-interpolation-uri-xpath-v1"

ENTRYPOINT ["/bin/sh", "/opt/neoserver/wcs20-interpolation-entrypoint.sh"]
CMD ["catalina.sh", "run"]
