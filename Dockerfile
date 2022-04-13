# Build go-neb
FROM golang:1.18-alpine as builder

RUN apk add --no-cache -t build-deps git gcc musl-dev go make g++

RUN git clone https://gitlab.matrix.org/matrix-org/olm.git /tmp/libolm \
    && cd /tmp/libolm \
    && make install

COPY . /tmp/go-neb
WORKDIR /tmp/go-neb
RUN go install honnef.co/go/tools/cmd/staticcheck@latest \
    && go install github.com/fzipp/gocyclo/cmd/gocyclo@latest \
    && go build github.com/matrix-org/go-neb

# Ensures we're lint-free
RUN /tmp/go-neb/hooks/pre-commit

# Run go-neb
FROM alpine:3.13

ENV BIND_ADDRESS=:4050 \
    DATABASE_TYPE=sqlite3 \
    DATABASE_URL=/data/go-neb.db?_busy_timeout=5000 \
    UID=1337 \
    GID=1337

COPY --from=builder /tmp/go-neb/go-neb /usr/local/bin/go-neb
# Copy libolm.so
COPY --from=builder /usr/local/lib/* /usr/local/lib/

RUN apk add --no-cache \
      libstdc++ \
      ca-certificates \
      su-exec \
      s6

VOLUME /data
EXPOSE 4050

COPY docker/root /

ENTRYPOINT ["/bin/s6-svscan", "/etc/s6.d/"]
