# Go-NEB
[![Build Status](https://github.com/matrix-org/go-neb/actions/workflows/ci.yaml/badge.svg?branch=master)](https://github.com/matrix-org/go-neb/actions/workflows/ci.yaml)

Go-NEB is a [Matrix](https://matrix.org) bot written in Go. It is the successor to [Matrix-NEB](https://github.com/matrix-org/Matrix-NEB), the original Matrix bot written in Python.

# Table of Contents
 * [Quick Start](#quick-start)
    * [Features](#features)
 * [Installing](#installing)
 * [Running](#running)
    * [Configuration file](#configuration-file)
 * [API](#api)
    * [Configuring clients](#configuring-clients)
    * [Configuring services](#configuring-services)
    * [Configuring realms](#configuring-realms)
    * [SAS verification](#sas-verification)
 * [Developing](#developing)
    * [Architecture](#architecture)
    * [API Docs](#viewing-the-api-docs)

# Quick Start

Clone and run (Requires Go 1.16+):

```bash
go build github.com/matrix-org/go-neb
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db?_busy_timeout=5000 BASE_URL=http://localhost:4050 ./go-neb
```

Get a Matrix user ID and access token. You can do this, for example, with the following curl command by replacing the user ID, password and Synapse URL with your own.

```bash
curl -X POST --header 'Content-Type: application/json' -d '{
    "identifier": { "type": "m.id.user", "user": "nebUsername" },
    "password": "nebPassword",
    "type": "m.login.password"
}' 'http://localhost:8008/_matrix/client/r0/login'
```

This is preferable to, for example, logging in via Riot and copying the access token and device ID from there, as then Riot will have uploaded its own device keys which Go-NEB won't have access to causing it to be unable to create encryption sessions.

The response of this command will be a JSON object with an access token and device ID.

Then, give the values to Go-NEB:

```bash
curl -X POST localhost:4050/admin/configureClient --data-binary '{
    "UserID": "@goneb:localhost",
    "HomeserverURL": "http://localhost:8008",
    "AccessToken": "<access_token>",
    "DeviceID": "<DEVICEID>",
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
 
### Guggy
 - Ability to query Guggy's gif engine.
 
### RSS Bot
 - Ability to read Atom/RSS feeds.
 
### Travis CI
 - Ability to receive incoming build notifications.
 - Ability to adjust the message which is sent into the room.
 
### Alertmanager
 - Ability to receive alerts and render them with go templates


# Installing

Go-NEB is built using Go 1.16+. Once you have installed Go, run the following commands:

```bash
# Clone the go-neb repository
git clone https://github.com/matrix-org/go-neb
cd go-neb

# Build go-neb
go build github.com/matrix-org/go-neb
```

# Running
Go-NEB uses environment variables to configure its SQLite database and bind address. To run Go-NEB, run the following command:
```bash
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db?_busy_timeout=5000 BASE_URL=https://public.facing.endpoint ./go-neb
```
 - `BIND_ADDRESS` is the port to listen on.
 - `DATABASE_TYPE` MUST be "sqlite3". No other type is supported.
 - `DATABASE_URL` is where to find the database file. One will be created if it does not exist. It is a URL so parameters can be passed to it. We recommend setting `_busy_timeout=5000` to prevent sqlite3 "database is locked" errors.
 - `BASE_URL` should be the public-facing endpoint that sites like Github can send webhooks to.
 - `CONFIG_FILE` is the path to the configuration file to read from. This isn't included in the example above, so Go-NEB will operate in HTTP mode.
 - `LOG_DIR` is a directory that log files will be written to, with log rotation enabled. If set, logging to stderr will be disabled.
Go-NEB needs to be "configured" with clients and services before it will do anything useful. It can be configured via a configuration file OR by an HTTP API.

## Configuration file
If you run Go-NEB with a `CONFIG_FILE` environment variable, it will load that file and use it for services, clients, etc. There is a [sample configuration file](config.sample.yaml) which explains all the options. In most cases, these are *direct mappings* to the corresponding HTTP API.

# API
The API is documented in sections using godoc. The sections consists of:
 - An HTTP API (the path and method to use)
 - A "JSON request body" (the JSON that is inside the HTTP request body)
 - "Configuration" information (any additional information that is specific to what you're creating)
 
To form the complete API, you need to combine the HTTP API with the JSON request body, and the "Configuration" information (which is always under a JSON key called `Config`). In addition, most APIs have a `Type` which determines which piece of code to load. To find out what the right type is for the thing you're creating, check the constants defined in godoc.

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

List of Services:
 - [Echo](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/echo/) - An example service
 - [Giphy](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/giphy/) - A GIF bot
 - [Github](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/github/) - A Github bot
 - [Github Webhook](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/github/index.html#WebhookService) - A Github notification bot
 - [Guggy](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/guggy/) - A GIF bot
 - [JIRA](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/jira/) - Integration with JIRA
 - [RSS Bot](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/rssbot/) - An Atom/RSS feed reader
 - [Travis CI](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/services/travisci/) - Receive build notifications from Travis CI


## Configuring Realms
Realms are how Go-NEB authenticates users on third-party websites.

 - [HTTP API Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/handlers/index.html#ConfigureAuthRealm.OnIncomingRequest)
 - [JSON Request Body Docs](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/api/index.html#ConfigureAuthRealmRequest)

List of Realms:
 - [Github](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/github/index.html#Realm)
 - [JIRA](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/jira/index.html#Realm)
 
Authentication via HTTP:
 - [Github](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/github/index.html#Realm.RequestAuthSession)
 - [JIRA](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/jira/index.html#Realm.RequestAuthSession)

Authentication via the config file:
 - [Github](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/github/index.html#Session)
 - [JIRA](https://matrix-org.github.io/go-neb/pkg/github.com/matrix-org/go-neb/realms/jira/index.html#Session)

## SAS verification
Go-NEB supports SAS verification using the decimal method. Another user can start a verification transaction with Go-NEB using their client, and it will be accepted. In order to confirm the devices, the 3 SAS integers must then be sent to Go-NEB, to the endpoint '/verifySAS' so that it can mark the device as trusted.

For example, if your user ID is `@user:localhost` and your device ID is `ABCD`, you start a SAS verification with Go-NEB and get the SAS "1111 2222 3333". You can perform the following curl request to let Go-NEB know the SAS integers so that it can match them with its own:

```bash
curl -X POST --header 'Content-Type: application/json' -d '{
    "UserID": "@neb:localhost",
    "OtherUserID": "@user:localhost",
    "OtherDeviceID": "ABCD",
    "SAS": [1111,2222,3333]
}' 'http://localhost:4050/verifySAS'
```

If the SAS match and you also confirm that via the other device's client, the verification should finish successfully.

# Contributing

Before submitting pull requests, please read the [Matrix.org contribution guidelines](https://github.com/matrix-org/synapse/blob/develop/CONTRIBUTING.md#sign-off) regarding sign-off of your work.

# Developing

This project depends on `libolm` for the end-to-end encryption. Therefore,
you need to install `libolm3` and `libolm-dev` on Ubuntu / `libolm-devel` on
CentOS to be able to build and run it.

There's a bunch more tools this project uses when developing in order to do
things like linting. Some of them are bundled with go (fmt and vet) but some
are not. You should install the ones which are not:

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
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

## Docker image

To get started quickly, use the image from docker.io:
```
docker run -v /path/to/data:/data -e "BASE_URL=http://your.public.url:4050" matrixdotorg/go-neb
```

If you'd prefer to build the file yourself, clone this repository and build the `Dockerfile`.

The image sets the following environment variables:
```
BIND_ADDRESS=:4050
DATABASE_TYPE=sqlite3
DATABASE_URL=/data/go-neb.db?_busy_timeout=5000
```

The image exposes port `4050` and a volume at `/data`. The `BASE_URL` environment variable needs to be set, a volume should be mounted at `/data` and port `4050` should be appropriately mapped as desired.
