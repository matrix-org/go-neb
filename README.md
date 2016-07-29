# Running goneb

Goneb uses environment variables to configure its database and bind address.
To run goneb:

    BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=goneb.db bin/goneb


Goneb needs to connect as a matrix user to receive messages. Goneb can listen
for messages as multiple matrix users. The users are configured using an
HTTP API and the config is stored in the database. Goneb will automatically
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

Services in goneb listen for messages in particular rooms using a given matrix
user. Services are configured using an HTTP API and the config is stored in the
database. Services use one of the matrix users configured on goneb to receive
matrix messages. Each service is configured to listen for messages in a set
of rooms. Goneb will automatically join the service to its rooms when it is
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

Goneb has a heartbeat listener that returns 200 OK so that load balancers can
check that the server is still running.

    curl -X GET localhost:4050/test

    {}
