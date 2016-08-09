# Building go-neb

Go-neb is built using `gb` (https://getgb.io/). To build go-neb:

```bash
# Install gb
go get github.com/constabulary/gb/...

# Clone the go-neb repository
git clone https://github.com/matrix-org/go-neb
cd go-neb

# Build go-neb
gb build github.com/matrix-org/go-neb
```

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
configured. To start an echo service:

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

## Starting a Github Service

### Register a Github realm

```
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "mygithubrealm",
    "Type": "github",
    "Config": {
        "ClientSecret": "YOUR_CLIENT_SECRET",
        "ClientID": "YOUR_CLIENT_ID",
        "RedirectBaseURI": "https://public.path.to.neb"
    }
}'
```
Returns:
```
{
  "ID":"mygithubrealm",
  "Type":"github",
  "OldConfig":null,
  "NewConfig":{
    "ClientSecret":"YOUR_CLIENT_SECRET",
    "ClientID":"YOUR_CLIENT_ID",
    "RedirectBaseURI":"https://public.path.to.neb"
  }
}
```

### Make a request for Github Auth

```
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@your_user_id:localhost",
    "Config": {
    }
}'
```
Returns:
```
{
  "URL":"https://github.com/login/oauth/authorize?client_id=$ID\u0026client_secret=$SECRET\u0026redirect_uri=$REDIRECT_BASE_URI%2Frealms%2Fredirects%2Fmygithubrealm\u0026state=$RANDOM_STRING"
}
```
Follow this link and grant access for NEB to act on your behalf.

### Create a github bot

```
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "github",
    "Id": "mygithubserviceid",
    "Config": {
    	"RealmID": "mygithubrealm",
        "BotUserID": "@goneb:localhost",
        "ClientUserID": "@example:localhost",
        "Rooms": {
        	"!EmwxeXCVubhskuWvaw:localhost": {
        		"Repos": {
        			"owner/repo": {
        				"Events": ["push","issues"]
        			}
        		}
        	}
        }
    }
}'
```

This request will make `BotUserID` join the `Rooms` specified and create webhooks for the `owner/repo` projects given.

# Developing on go-neb.

There's a bunch more tools this project uses when developing in order to do
things like linting. Some of them are bundled with go (fmt and vet) but some
are not. You should install the ones which are not:

```bash
go get github.com/golang/lint/golint
go get github.com/fzipp/gocyclo
```

You can then install the pre-commit hook:

```bash
./hooks/install.sh
```

## Viewing the API docs.

```
# Start a documentation server listening on :6060
GOPATH=$GOPATH:$(pwd) godoc -v -http=localhost:6060 &

# Open up the documentation for go-neb in a browser.
sensible-browser http://localhost/pkg/github.com/matrix-org/go-neb
```
