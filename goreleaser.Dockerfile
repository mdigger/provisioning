FROM alpine:latest as alpine
RUN apk add -U --no-cache ca-certificates

FROM scratch
LABEL maintainer="dmitrys@xyzrd.com" \
org.label-schema.name="Provisioning Service" \
org.label-schema.description="Provisioning Service" \
org.label-schema.vendor="xyzrd.com" \
org.label-schema.vcs-url="https://github.com/Connector73/provisioning" \
org.label-schema.schema-version="1.0"
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY provisioning /
ENV PORT="8000" LOG=""
EXPOSE ${PORT}
VOLUME ["/letsEncrypt.cache", "/certs", "/db"]
ENTRYPOINT ["/provisioning"]
