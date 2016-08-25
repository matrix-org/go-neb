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

    BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db BASE_URL=https://public.facing.endpoint bin/go-neb


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
        "UserID": "@goneb:localhost:8448",
        "Config": {
        }
    }'
    {
        "Type": "echo",
        "Id": "myserviceid",
        "UserID": "@goneb:localhost:8448",
        "OldConfig": {},
        "NewConfig": {}
    }
    
To retrieve an existing Service:

    curl -X POST localhost:4050/admin/getService --data-binary '{
        "Id": "myserviceid"
    }'
    {
        "Type": "echo",
        "Id": "myserviceid",
        "UserID": "@goneb:localhost:8448",
        "Config": {}
    }

Go-neb has a heartbeat listener that returns 200 OK so that load balancers can
check that the server is still running.

    curl -X GET localhost:4050/test

    {}
    
## Architecture

```

   HOMESERVER
       |
+=============================================================+
|      |                 Go-NEB                               |
| +---------+                                                 |
| | Clients |                                                 |
| +---------+                                                 |
|      |                                                      |
| +---------+       +------------+      +--------------+      |
| | Service |-------| Auth Realm |------| Auth Session |-+    |
| +---------+       +------------+      +--------------+ |    |
|     ^                   ^              +---------------+    |
|     |                   |                                   |
+=============================================================+
      |                   |                   
    WEBHOOK            REDIRECT
    REQUEST            REQUEST
    
    
Clients      = A thing which can talk to homeservers and listen for events.
Service      = An individual bot, configured by a user.
Auth Realm   = A place where a user can authenticate with.
Auth Session = An individual authentication session


```

Some `AuthRealms` support "Starter Links". These are HTTP URLs which knowledgeable clients should use to *start* the auth process. They are commonly returned as metadata to `!commands`.
These links require the client to prove that they own a given user ID by appending a token
to the Starter Link. This token will be used to verify the client's identity by making an
Open ID request to the user's Homeserver via federation.

## Starting a Github Service

### Register a Github realm

This API allows for an optional `StarterLink` value.

```
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "mygithubrealm",
    "Type": "github",
    "Config": {
        "ClientSecret": "YOUR_CLIENT_SECRET",
        "ClientID": "YOUR_CLIENT_ID",
        "StarterLink": "https://example.com/requestGithubOAuthToken"
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
    "StarterLink": "https://example.com/requestGithubOAuthToken"
  }
}
```

### Make a request for Github Auth

```
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@your_user_id:localhost",
    "Config": {
        "RedirectURL": "https://optional-url.com/to/redirect/to/after/auth"
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
    "UserID": "@goneb:localhost",
    "Config": {
    	"RealmID": "mygithubrealm",
        "ClientUserID": "@example:localhost",
        "HandleCommands": true,
        "HandleExpansions": true,
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


## Starting a JIRA Service

### Register a JIRA realm

Generate an RSA private key: (JIRA does not support key sizes >2048 bits)

```bash
openssl genrsa -out privkey.pem 2048
cat privkey.pem
```

This API allows for an optional `StarterLink` value. Create the realm:

```
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "jirarealm",
    "Type": "jira",
    "Config": {
        "JIRAEndpoint": "matrix.org/jira/",
        "StarterLink": "https://example.com/requestJIRAOAuthToken",
        "ConsumerName": "goneb",
        "ConsumerKey": "goneb",
        "ConsumerSecret": "random_long_string",
        "PrivateKeyPEM": "-----BEGIN RSA PRIVATE KEY-----\r\nMIIEowIBAAKCAQEA39UhbOvQHEkBP9fGnhU+eSObTWBDGWygVYzbcONOlqEOTJUN\r\n8gmnellWqJO45S4jB1vLLnuXiHqEWnmaShIvbUem3QnDDqghu0gfqXHMlQr5R8ZP\r\norTt1F2idWy1wk5rVXeLKSG7uriYhDVOVS69WuefoW5v55b5YZV283v2jROjxHuj\r\ngAsJA7k6tvpYiSXApUl6YHmECfBoiwG9bwItkHwhZ\/fG9i4H8\/aOyr3WlaWbVeKX\r\n+m38lmYZvzQFRAk5ab1vzCGz4cyc\r\nTk2qmZpcjHRd1ijcOkgC23KF8lHWF5Zx0tySR+DWL1JeGm8NJxKMRJZuE8MIkJYF\r\nryE7kjspNItk6npkA3\/A4PWwElhddI4JpiuK+29mMNipRcYYy9e0vH\/igejv7ayd\r\nPLCRMQKBgBDSNWlZT0nNd2DXVqTW9p+MG72VKhDgmEwFB1acOw0lpu1XE8R1wmwG\r\nZRl\/xzri3LOW2Gpc77xu6fs3NIkzQw3v1ifYhX3OrVsCIRBbDjPQI3yYjkhGx24s\r\nVhhZ5S\/TkGk3Kw59bDC6KGqAuQAwX9req2l1NiuNaPU9rE7tf6Bk\r\n-----END RSA PRIVATE KEY-----"
    }
}'
```

The following keys will be modified/added:
 - `JIRAEndpoint` in canonicalised form.
 - `Server` and `Version` keys which are purely informational for the caller.
 - `PublicKeyPEM` which the caller needs a human to insert into the JIRA Application Links web form.


Returns:

```json
{
    "ID": "jirarealm",
    "Type": "jira",
    "OldConfig": null,
    "NewConfig": {
        "JIRAEndpoint": "https://matrix.org/jira/",
        "StarterLink": "https://example.com/requestJIRAOAuthToken",
        "Server": "Matrix.org",
        "Version": "6.3.5a",
        "ConsumerName": "goneb",
        "ConsumerKey": "goneb",
        "ConsumerSecret": "random_long_string",
        "PublicKeyPEM": "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA39UhbOvQHEkBP9fGnhU+\neSObTWBDGWygVYzbcONOlqEOTJUN8gmnellWqJO45S4jB1vLLnuXiHqEWnmaShIv\nbUem3QnDDqghu0gfqXHMlQr5R8ZPorTt1F2idWy1wk5rVXeLKSG7uriYhDVOVS69\nWuefoW5v55b5YZV283v2jROjxHujgAsJA7k6tvpYiSXApUl6YHmECfBoiwG9bwIt\nkHwhZ/fG9i4H8/aOyr3WlaWbVeKX+m38lmYZvzQFRd7UPU7DuO6Aiqj7RxrbAvqq\ndPeoAvo6+V0TRPZ8YzKp2yQmDcGH69IbuKJ2BG1Qx8znZAvghKQ6P9Im+M4c7j9i\ndwIDAQAB\n-----END PUBLIC KEY-----\n",
        "PrivateKeyPEM": "-----BEGIN RSA PRIVATE KEY-----\r\nMIIEowIBAAKCAQEA39UhbOvQHEkBP9fGnhU+eSObTWBDGWygVYzbcONOlqEOTJUN\r\n8gmnellWqJO45S4jB1vLLnuXiHqEWnmaShIvbUem3QnDDqghu0gfqXHMlQr5R8ZP\r\norTt1F2idWy1wk5rVXeLKSG7uriYhDVOVS69WuefoW5v55b5YZV283v2jROjxHuj\r\ngAsJA7k6tvpYiSXApUl6YHmECfBoiwG9bwItkHwhZ/fG9i4H8/aOyr3WlaWbVeKX\r\n+m38lmYZvzQFRd7UPU7DuO6Aiqj7RxrbAvqqdPeoAvo6+V0TRPZ8YzKp2yQmDcGH\r\n69IbuKJ2BG1Qx8znZAvghKQ6P9Im+M4c7j9iMG72VKhDgmEwFB1acOw0lpu1XE8R1wmwG\r\nZRl/xzri3LOW2Gpc77xu6fs3NIkzQw3v1ifYhX3OrVsCIRBbDjPQI3yYjkhGx24s\r\nVhhZ5S/TkGk3Kw59bDC6KGqAuQAwX9req2l1NiuNaPU9rE7tf6Bk\r\n-----END RSA PRIVATE KEY-----"
    }
}
```

The `ConsumerKey`, `ConsumerSecret`, `ConsumerName` and `PublicKeyPEM` must be manually inserted
into the "Application Links" section under JIRA Admin Settings by a JIRA admin on the target
JIRA installation. Once that is complete, users can OAuth on the target JIRA installation.


### Make a request for JIRA Auth

```
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "jirarealm",
    "UserID": "@example:localhost",
    "Config": {
        "RedirectURL": "https://optional-url.com/to/redirect/to/after/auth"
    }
}'
```
Returns:
```json
{
    "URL":"https://jira.somewhere.com/plugins/servlet/oauth/authorize?oauth_token=7yeuierbgweguiegrTbOT"
}
```

Follow this link and grant access for NEB to act on your behalf.

### Create a JIRA bot

```
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "jira",
    "Id": "jid",
    "UserID": "@goneb:localhost",
    "Config": {
        "ClientUserID": "@example:localhost",
        "Rooms": {
            "!EmwxeXCVubhskuWvaw:localhost": {
                "Realms": {
                    "jira_realm_id": {
                        "Projects": {
                            "BOTS": {
                                "Expand": true,
                                "Track": true
                            }
                        }
                    }
                }
            }
        }
    }
}'
```

## Starting a Giphy Service

### Create a Giphy bot

```
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "giphy",
    "Id": "giphyid",
    "UserID": "@goneb:localhost",
    "Config": {
        "APIKey": "YOUR_API_KEY"
    }
}'
```


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
