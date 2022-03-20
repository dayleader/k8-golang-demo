# Kubernetes Golang gRPC demo

This repository contains a simple application that demonstrates how to deploy a simple golang application to kubernetes.

## Features

  * No need to install protoc compiler because demo using docker-protoc
  * An example of using gRPC Gateway
  * Google APIs included
  * Kubernetes deployment example
  * MySQL data migration example

## Project structure

  * cmd/main.go - main application
  * proto/* - contains a description of application proto files
  * gen/* - contains generated proto files
  * third_party/* - contains third party api's
  * docker-compose.yml - contains a description of gRPC/Protocol buffer compiler containers

## Usage

Start the application:

```sh
$ make run
```
or

```sh
$ go run cmd/main.go
```

Create user via grpc-gateway:

```sh
$ curl -d '{"name":"John", "type":1}' -H "Content-Type: application/json" -X POST http://localhost:8080/v1/users
```

Get user via grpc-gateway:

```sh
$ curl -H "Content-Type: application/json" -X GET http://localhost:8080/v1/users?id=${USER_ID}
```

Compile protobuffs:

```sh
$ make compile-pb
```
or

```sh
$ docker-compose -f docker-compose.yml up
```
