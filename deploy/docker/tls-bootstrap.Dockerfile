FROM alpine:3.22

RUN apk add --no-cache openssl
COPY deploy/docker/bootstrap-local-tls.sh /usr/local/bin/bootstrap-local-tls.sh
COPY deploy/docker/bootstrap-agent-grpc-tls.sh /usr/local/bin/bootstrap-agent-grpc-tls.sh
RUN chmod 0755 /usr/local/bin/bootstrap-local-tls.sh /usr/local/bin/bootstrap-agent-grpc-tls.sh

ENTRYPOINT ["/usr/local/bin/bootstrap-local-tls.sh"]
