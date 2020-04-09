# Build go-neb
FROM golang:1.14-alpine as builder

RUN apk add --no-cache -t build-deps git gcc musl-dev go

COPY . /tmp/go-neb
WORKDIR /tmp/go-neb
RUN go get golang.org/x/lint/golint \
    && go get github.com/fzipp/gocyclo \
    && go build github.com/matrix-org/go-neb

# Ensures we're lint-free
RUN /tmp/go-neb/hooks/pre-commit

# Run go-neb
FROM alpine:3.7

ENV BIND_ADDRESS=:4050 \
    DATABASE_TYPE=sqlite3 \
    DATABASE_URL=/data/go-neb.db?_busy_timeout=5000 \
    UID=1337 \
    GID=1337

COPY --from=builder /tmp/go-neb/go-neb /usr/local/bin/go-neb
RUN apk add --no-cache \
      ca-certificates \
      su-exec \
      s6

VOLUME /data
EXPOSE 4050

COPY docker/root /

ENTRYPOINT ["/bin/s6-svscan", "/etc/s6.d/"]
