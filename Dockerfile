FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY telnyx-mock .
ENTRYPOINT ["/telnyx-mock", "-http-port", "12111", "-https-port", "12112"]
EXPOSE 12111
EXPOSE 12112
