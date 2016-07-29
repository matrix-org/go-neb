# Running go-neb

Go-neb uses environment variables to configure its database and bind address.
To run go-neb:

    BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db bin/go-neb


Go-neb needs to connect as a matrix user to receive messages. Go-neb can listen
for messages as multiple matrix users. The users are configured using an
HTTP API and the config is stored in the database. Go-neb will automatically
start syncing matrix messages when the user is configured. To create a user:

    curl -X POST localhost:4050/admin/configureClient --data-binary '{
        "UserID": "@goneb:localhost:8448",
        "HomeserverURL": "http://localhost:8008",
        "AccessToken": "<access_token>"
    }'
    {
        "OldClient": {},
        "NewClient": {
            "UserID": "@goneb:localhost:8448",
            "HomeserverURL": "http://localhost:8008",
            "AccessToken": "<access_token>"
        }
    }

Services in go-neb listen for messages in particular rooms using a given matrix
user. Services are configured using an HTTP API and the config is stored in the
database. Services use one of the matrix users configured on go-neb to receive
matrix messages. Each service is configured to listen for messages in a set
of rooms. Go-neb will automatically join the service to its rooms when it is
configured. To start a service:

    curl -X POST localhost:4050/admin/configureService --data-binary '{
        "Type": "echo",
        "Id": "myserviceid",
        "Config": {
            "UserID": "@goneb:localhost:8448",
            "Rooms": ["!QkdpvTwGlrptdeViJx:localhost:8448"]
        }
    }'
    {
        "Type": "echo",
        "Id": "myserviceid",
        "OldConfig": {},
        "NewConfig": {
            "UserID": "@goneb:localhost:8448",
            "Rooms": ["!QkdpvTwGlrptdeViJx:localhost:8448"]
        }
    }

Go-neb has a heartbeat listener that returns 200 OK so that load balancers can
check that the server is still running.

    curl -X GET localhost:4050/test

    {}
