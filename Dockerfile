FROM alpine:3.6

ENV BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=/data/go-neb.db?_busy_timeout=5000

COPY bin/go-neb /go-neb

VOLUME /data
EXPOSE 4050

ENTRYPOINT ["/go-neb"]
