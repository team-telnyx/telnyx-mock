# -*- mode: dockerfile -*-
#
# A multi-stage Dockerfile that builds a Linux target then creates a small
# final image for deployment.

#
# STAGE 1
#
# Uses a Go image to build a release binary.
#

FROM golang:1.9-alpine AS builder
WORKDIR /go/src/github.com/team-telnyx/telnyx-mock/
COPY ./ ./
RUN set -o nounset -o xtrace \
    && apk --no-cache add \
        git \
    \
    # get what you need to make 'Asset's work  \
    && go get -u github.com/jteeuwen/go-bindata/... \
    # && git -C openapi/ pull origin master \
    # ^^^ stripe has a separate openapi repo that was a submodule of this
    && go generate \
    \
    && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o telnyx-mock .

#
# STAGE 2
#
# Use a tiny base image (alpine) and copy in the release target. This produces
# a very small output image for deployment.
#

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /go/src/github.com/team-telnyx/telnyx-mock/telnyx-mock .
ENTRYPOINT ["/telnyx-mock", "-http-port", "12111", "-https-port", "12112"]
EXPOSE 12111
EXPOSE 12112
