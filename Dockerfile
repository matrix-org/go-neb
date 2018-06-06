FROM alpine:3.6

ENV BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=/data/go-neb.db?_busy_timeout=5000

COPY . /tmp/go-neb
WORKDIR /tmp/go-neb
ENV GOPATH=/tmp/go-neb/vendor/src:/tmp/go-neb/vendor:/tmp/go-neb
RUN apk add --no-cache -t build-deps git gcc musl-dev go \
    && go get -u github.com/constabulary/gb/... \
    && export PATH="/tmp/go-neb/vendor/src/bin:${PATH}" \
    && gb vendor restore \
    && gb build -f github.com/matrix-org/go-neb \
    && mv bin/go-neb /go-neb \
    && cd / \
    && rm -rf /tmp/* \
    && apk del build-deps

VOLUME /data
EXPOSE 4050

ENTRYPOINT ["/go-neb"]
