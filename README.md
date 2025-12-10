# archived-transcript-server
A Go HTTP server that stores and presents transcripts to all web clients.

## Overview

_System_

- **[Archived Transcript System](#archived-transcript-system)**

_Development_

- **[Tech Used](#tech-used)**
- **[Requirements](#requirements)**
- **[Running Source Code](#running-source-code)**
- **[Debugging/Logging](#debugginglogging)**

_Docker_
- **[Host Requirements](#host-requirements)**
- **[Version Guide](#version-guide)**
- **[Running with Docker](#running-with-docker)**

## System

### Archived Transcript System

Archived Transcript is a system that contains three programs:

- Data: [dokiscripts-data](https://github.com/duckautomata/dokiscripts-data)
- Server: [archived-transcript-server](https://github.com/duckautomata/archived-transcript-server)
- Client: [archived-transcript](https://github.com/duckautomata/archived-transcript)

All three programs work together to transcribe all streams/videos/content and allows anyone to search through and view them.

- Data will transcribe the content, stores the `.srt` files in git for safekeeping, and uploads the `.srt` files to the server. All of these steps are manually triggered.
- Server (this) will receive `.srt` files from Data and store them into a database. Upon request from the Client, it will search through the data base and return the requested data.
- Client is the UI that renders the transcript for us to use.

## Development

### Tech Used
- Go 1.25
- SQL

### Requirements
- [Go](https://go.dev/doc/install)
- Any OS

### Running Source Code

**NOTE**: This is only required to run the source code. If you only want to run it and not develop it, then check out the [Docker seciton](#docker)

1. Download and install Go
2. Referencing `config-example.yaml`, create `config.yaml` and add your specific configurations.
5. Download dependencies `go mod download`

When all of that is done, you can run `scripts/run.sh` (or just `go run ./cmd/web/` from the root directory) to start archived-transcript-server.

### Debugging/Logging

Logging is set up for the entire program, and everything should be logged. The console will print info and higher logs (everything but debug). On startup, a log file under `tmp/` will be created and will contain every log. In the event of an error, check this log file to see what went wrong.


## Docker

### Host Requirements
- Any OS
- Docker

If it has Docker, it can run this.

### Version Guide
Uses an x.y major.minor version standard.

Major version is used to denote any API/breaking changes.

Minor version is used to denote any code/dependency changes that do not break anything.

Tags:
- `latest` will always be the most recent image.
- `x` will be the latest x major version image. Meaning, if the tag is `2` and the latest `2.y` image is `2.10`, then `2` will use the `2.10` image. When a new `2.11` image is created, then the tag `2` will use that new image.
- `x.y` will be a specific image.

The major version between Worker and Server _should_ remain consistent.

You can view all tags on [Dockerhub](https://hub.docker.com/r/duckautomata/archived-transcript-server/tags)

### Running with Docker
The easiest way to run the docker image is to
1. clone this repo locally
2. create `config.yaml` from the example config file, adding in your specific configurations.
3. then run `./docker/start.sh`

If there are permission errors and the container cannot write to tmp/, then you first need to run `sudo chmod -R 777 tmp` to give the container permissions.

Depending on your use case, you can change the configuration variables in `start.sh` to match your needs.

Logs and current state are stored in the `tmp/` folder outside the container. Because of this, state is not lost on restart.

**Note**: the docker container and the source code use the same `tmp/` folder to store runtime data. Because of this, you are required to run either or, but not both. If you want to run both development and a docker image, then use separate folders.

### Metrics
This project uses Prometheus to aggregate server metrics.
