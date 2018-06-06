# Build go-neb
FROM golang:1.10-alpine as builder

COPY . /tmp/go-neb
WORKDIR /tmp/go-neb
RUN apk add --no-cache -t build-deps git gcc musl-dev go \
    && go get -u github.com/constabulary/gb/... \
    && gb vendor restore \
    && gb build -f github.com/matrix-org/go-neb


# Run go-neb
FROM alpine:3.6

ENV BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=/data/go-neb.db?_busy_timeout=5000

COPY --from=builder /tmp/go-neb/bin/go-neb /go-neb
RUN apk add --no-cache ca-certificates

VOLUME /data
EXPOSE 4050

ENTRYPOINT ["/go-neb"]
