FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS builder
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
COPY . /go/src/github.com/singlink/singlink
WORKDIR /go/src/github.com/singlink/singlink
ARG TARGETOS TARGETARCH
ARG GOPROXY=""
ARG VERSION=""
ENV GOPROXY ${GOPROXY}
ENV CGO_ENABLED=0
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN set -ex \
    && apk add git build-base \
    && if [ -z "$VERSION" ]; then VERSION=$(go run ./cmd/internal/read_tag); fi \
    && export TAGS=$(cat release/DEFAULT_BUILD_TAGS_OTHERS) \
    && export LDFLAGS_SHARED=$(cat release/LDFLAGS) \
    && go build -v -trimpath -tags "$TAGS" \
        -o /go/bin/singlink \
        -ldflags "-X \"github.com/singlink/singlink/constant.Version=$VERSION\" $LDFLAGS_SHARED -s -w -buildid=" \
        ./cmd/singlink
FROM --platform=$TARGETPLATFORM alpine AS dist
LABEL maintainer="nekohasekai <contact-git@sekai.icu>"
RUN set -ex \
    && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=builder /go/bin/singlink /usr/local/bin/singlink
ENTRYPOINT ["singlink"]
