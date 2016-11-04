# Go-NEB

Go-NEB is a [Matrix](https://matrix.org) bot written in Go. It is the successor to [Matrix-NEB](https://github.com/matrix-org/Matrix-NEB), the original Matrix bot written in Python.

# Table of Contents
 * [Quick Start](#quick-start)
    * [Features](#features)
 * [Installing](#installing)
 * [Running](#running)
    * [Configuration file](#configuration-file)
    * [Configuring clients](#configuring-clients)
    * [Configuring services](#configuring-services)
        * [Echo Service](#echo-service)
        * [Github Service](#github-service)
        * [Github Webhook Service](#github-webhook-service)
        * [JIRA Service](#jira-service)
        * [Giphy Service](#giphy-service)
    * [Configuring realms](#configuring-realms)
        * [Github Realm](#github-realm)
           * [Github Authentication](#github-authentication)
        * [JIRA Realm](#jira-realm)
 * [Developing](#developing)
    * [Architecture](#architecture)
    * [API Docs](#viewing-the-api-docs)

# Quick Start

Clone and run (Requires Go 1.5+ and GB):

```bash
gb build github.com/matrix-org/go-neb
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db?_busy_timeout=5000 BASE_URL=http://localhost:4050 bin/go-neb
```

Get a Matrix user ID and access token and give it to Go-NEB:

```bash
curl -X POST localhost:4050/admin/configureClient --data-binary '{
    "UserID": "@goneb:localhost",
    "HomeserverURL": "http://localhost:8008",
    "AccessToken": "<access_token>",
    "Sync": true,
    "AutoJoinRooms": true,
    "DisplayName": "My Bot"
}'
```

Tell it what service to run:

```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "echo",
    "Id": "myserviceid",
    "UserID": "@goneb:localhost",
    "Config": {}
}'
```

Invite the bot user into a Matrix room and type `!echo hello world`. It will reply with `hello world`.


## Features

### Github
 - Login with OAuth2.
 - Ability to create Github issues on any project.
 - Ability to track updates (add webhooks) to projects. This includes new issues, pull requests as well as commits.
 - Ability to expand issues when mentioned as `foo/bar#1234`.
 - Ability to assign a "default repository" for a Matrix room to allow `#1234` to automatically expand, as well as shorter issue creation command syntax.

### JIRA
 - Login with OAuth1.
 - Ability to create JIRA issues on a project.
 - Ability to expand JIRA issues when mentioned as `FOO-1234`.

### Giphy
 - Ability to query Giphy's "text-to-gif" engine.


# Installing
Go-NEB is built using Go 1.5+ and [GB](https://getgb.io/). Once you have installed Go, run the following commands:
```bash
# Install gb
go get github.com/constabulary/gb/...

# Clone the go-neb repository
git clone https://github.com/matrix-org/go-neb
cd go-neb

# Build go-neb
gb build github.com/matrix-org/go-neb
```

# Running
Go-NEB uses environment variables to configure its SQLite database and bind address. To run Go-NEB, run the following command:
```bash
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db?_busy_timeout=5000 BASE_URL=https://public.facing.endpoint bin/go-neb
```
 - `BIND_ADDRESS` is the port to listen on.
 - `DATABASE_TYPE` MUST be "sqlite3". No other type is supported.
 - `DATABASE_URL` is where to find the database file. One will be created if it does not exist. It is a URL so parameters can be passed to it. We recommend setting `_busy_timeout=5000` to prevent sqlite3 "database is locked" errors.
 - `BASE_URL` should be the public-facing endpoint that sites like Github can send webhooks to.
 - `CONFIG_FILE` is the path to the configuration file to read from. This isn't included in the example above, so Go-NEB will operate in HTTP mode.

Go-NEB needs to be "configured" with clients and services before it will do anything useful. It can be configured via a configuration file OR by an HTTP API.

## Configuration file
If you run Go-NEB with a `CONFIG_FILE` environment variable, it will load that file and use it for services, clients, etc. There is a [sample configuration file](config.sample.yaml) which explains all the options. In most cases, these are *direct mappings* to the corresponding HTTP API.

## Configuring Clients
Go-NEB needs to connect as a matrix user to receive messages. Go-NEB can listen for messages as multiple matrix users. The users are configured using an HTTP API and the config is stored in the database.

 - [HTTP API Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/handlers/index.html#ConfigureClient.OnIncomingRequest)
 - [JSON Request Body Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/index.html#ClientConfig)

## Configuring Services
Services contain all the useful functionality in Go-NEB. They require a client to operate. Services are configured using an HTTP API and the config is stored in the database. Services use one of the matrix users configured on Go-NEB to send/receive matrix messages.

Every service has an "ID", "type" and "user ID". Services may specify additional "config" keys: see the specific
service you're interested in for the additional keys, if any.

 - [HTTP API Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/handlers/index.html#ConfigureService.OnIncomingRequest)
 - [JSON Request Body Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/index.html#ConfigureServiceRequest)

List of services:
 - [Echo](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/echo/) - An example service
 - [Giphy](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/giphy/) - A GIF bot
 - [Github](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/github/) - A Github bot
 - [Github Webhook](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/github/index.html#WebhookService) - A Github notification bot
 - [Guggy](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/guggy/) - A GIF bot
 - [JIRA](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/jira/) - Integration with JIRA
 - [RSS Bot](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/rssbot/) - An Atom/RSS feed reader


## Configuring Realms
Realms are how Go-NEB authenticates users on third-party websites.

 - [HTTP API Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/handlers/index.html#ConfigureAuthRealm.OnIncomingRequest)
 - [JSON Request Body Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/index.html#ConfigureAuthRealmRequest)

### Github Realm
This has the `Type` of `github`. To set up this realm:
```bash
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
 - `ClientSecret`: Your Github application client secret
 - `ClientID`: Your Github application client ID
 - `StarterLink`: Optional. If supplied, `!github` commands will return this link whenever someone is prompted to login to Github.

#### Github authentication
Once you have configured a Github realm, you can associate any Matrix user ID with any Github user. To do this:
```bash
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@real_matrix_user:localhost",
    "Config": {
        "RedirectURL": "https://optional-url.com/to/redirect/to/after/auth"
    }
}'
```
 - `UserID`: The Matrix user ID to associate with.
 - `RedirectURL`: Optional. The URL to redirect to after authentication.

This request will return an OAuth URL:
```json
{
  "URL": "https://github.com/login/oauth/authorize?client_id=abcdef&client_secret=acascacac...."
}
```

Follow this link to associate this user ID with this Github account. Once this is complete, Go-NEB will have an OAuth token for this user ID and will be able to create issues as their real Github account.

To remove this session:

```bash
curl -X POST localhost:4050/admin/removeAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@real_matrix_user:localhost",
    "Config": {}
}'
```
 
### JIRA Realm
This has the `Type` of `jira`. To set up this realm:
```bash
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "jirarealm",
    "Type": "jira",
    "Config": {
        "JIRAEndpoint": "matrix.org/jira/",
        "ConsumerName": "goneb",
        "ConsumerKey": "goneb",
        "ConsumerSecret": "random_long_string",
        "PrivateKeyPEM": "-----BEGIN RSA PRIVATE KEY-----\r\nMIIEowIBAAKCAQEA39UhbOvQHEkBP9fGnhU+eSObTWBDGWygVYzbcONOlqEOTJUN\r\n8gmnellWqJO45S4jB1vLLnuXiHqEWnmaShIvbUem3QnDDqghu0gfqXHMlQr5R8ZP\r\norTt1F2idWy1wk5rVXeLKSG7uriYhDVOVS69WuefoW5v55b5YZV283v2jROjxHuj\r\ngAsJA7k6tvpYiSXApUl6YHmECfBoiwG9bwItkHwhZ\/fG9i4H8\/aOyr3WlaWbVeKX\r\n+m38lmYZvzQFRAk5ab1vzCGz4cyc\r\nTk2qmZpcjHRd1ijcOkgC23KF8lHWF5Zx0tySR+DWL1JeGm8NJxKMRJZuE8MIkJYF\r\nryE7kjspNItk6npkA3\/A4PWwElhddI4JpiuK+29mMNipRcYYy9e0vH\/igejv7ayd\r\nPLCRMQKBgBDSNWlZT0nNd2DXVqTW9p+MG72VKhDgmEwFB1acOw0lpu1XE8R1wmwG\r\nZRl\/xzri3LOW2Gpc77xu6fs3NIkzQw3v1ifYhX3OrVsCIRBbDjPQI3yYjkhGx24s\r\nVhhZ5S\/TkGk3Kw59bDC6KGqAuQAwX9req2l1NiuNaPU9rE7tf6Bk\r\n-----END RSA PRIVATE KEY-----"
    }
}'
```
 - `JIRAEndpoint`: The base URL of the JIRA installation you wish to talk to.
 - `ConsumerName`: The desired "Consumer Name" field of the "Application Links" admin page on JIRA. Generally this is the name of the service. Users will need to enter this string into their JIRA admin web form.
 - `ConsumerKey`: The desired "Consumer Key" field of the "Application Links" admin page on JIRA. Generally this is the name of the service. Users will need to enter this string into their JIRA admin web form.
 - `ConsumerSecret`: The desired "Consumer Secret" field of the "Application Links" admin page on JIRA. This should be a random long string. Users will need to enter this string into their JIRA admin web form.
 - `PrivateKeyPEM`: A string which contains the private key for performing OAuth 1.0 requests. This MUST be in PEM format. It must NOT have a password. Go-NEB will convert this into a **public** key in PEM format and return this to users. Users will need to enter the public key into their JIRA admin web form.
 - `StarterLink`: Optional. If supplied, `!jira` commands will return this link whenever someone is prompted to login to JIRA.

To generate a private key PEM: (JIRA does not support bit lengths >2048)
```bash
openssl genrsa -out privkey.pem 2048
cat privkey.pem
```

#### JIRA authentication

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

# Developing
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
    
    
Clients      = A thing which can talk to homeservers and listen for events. /configureClient makes these.
Service      = An individual bot, configured by a user. /configureService makes these.
Auth Realm   = A place where a user can authenticate with. /configureAuthRealm makes these.
Auth Session = An individual authentication session /requestAuthSession makes these.

```


## Viewing the API docs

The full docs can be found on [Github Pages](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb). Alternatively, you can locally host the API docs:

```
# Start a documentation server listening on :6060
GOPATH=$GOPATH:$(pwd) godoc -v -http=localhost:6060 &

# Open up the documentation for go-neb in a browser.
sensible-browser http://localhost:6060/pkg/github.com/matrix-org/go-neb
```
