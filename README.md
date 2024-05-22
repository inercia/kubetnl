# kubetnl

kubetnl (*kube tunnel*) is library (and command line utility) to tunnel
TCP connections from within a Kubernetes to a cluster-external endpoint,
e.g. to your local machine.

You can think of it as doing the opposite of `kubectl port-forward`.

For example, it can be used for:

* starting a local HTTP server in your program
* exposing it in a Kubernetes service
* sending all the traffic from that Kubernetes service to your local HTTP server

and doing all of that programatically.

## Demo

![kubetnl-demo.gif](https://gist.githubusercontent.com/fischor/6d175f01db8ded817d5fc72dcd37811e/raw/d5a708324354b49fa5dd15c47f9fd52287c394e1/kubetnl.gif)

## How does the command line tool work?

When a new tunnel with `kubetnl tunnel myservice 8080:8080 9090:9090 5.5.5.5:80:8888`
is created, kubetnl will create a `Service` and a `Pod` with name `myservice` in
your cluster.

The `Pod` will expose port `8080`, `9090`, `8888` and another random port for
the tunnel. The `Service` will target port `8080`, `9090` and `8888` of the Pod.

Once the `Pod` is running, a connection to the `Pod`'s exposed port for tunneling
is established via pod portforwarding.

Using the established connection to the pod, `kubetnl` opens a tunnel on the `Pod`
causing it to forward any incoming connections on port `8080`, `9090` and `8888` to
the kubetnl binary.

From the `kubetnl` binary, the connections are then forwarded to their specified target endpoints.

## Installation

### Install using `go install`

If you have Go installed, you can simply run

```
go install github.com/inercia/kubetnl@latest
```

to install the latest version of `kubetnl`. 

You can replace latest with the version tag you want to install, e.g. `v0.1.1`.

### Install by picking a relase

Go the the [release section](https://github.com/inercia/kubetnl/releases) and pick the binary for your operation system and architecture and add it to your PATH.

## Prerequisities

For `kubetnl` to work, you need to have privilidges the create services and pods and to do portforwarding on pods. 
Your cluster must also be able to pull the docker.io/fischor/kubetnl-server image. 

## Commands

### `kubetnl --help`

```sh
$ kubetnl --help
kubetnl tunnels TCP connections from within a Kubernetes cluster to an external endpoint.

 Find more information and check out the souce code at: https://github.com/inercia/kubetnl

Basic commands
  tunnel      Setup a new tunnel
  cleanup     Delete all resources created by kubetnl

Other Commands:
  completion  generate the autocompletion script for the specified shell
  version     Print the kubetnl version

Usage:
  kubetnl [flags] [options]

Use "kubetnl <command> --help" for more information about a given command.
Use "kubetnl options" for a list of global command-line options (applies to all commands).
```

### `kubetnl tunnel --help`

```sh
$ kubetnl tunnel --help
Setup a new tunnel.

 A tunnel forwards connections directed to a Kubernetes Service port within a cluster to an endpoint outside of the
cluster, e.g. to your local machine.

 Under the hood "kubetnl tunnel" creates a new service and pod that expose the specified ports. Any incoming connections
to an exposed port of the newly created service/pod will be tunneled to the endpoint specified for that port.

 "kubetnl tunnel" runs in the foreground. To stop press CTRL+C once. This will gracefully shutdown all active
connections and cleanup the created resources in the cluster before exiting.

Examples:
  # Tunnel to local port 8080 from myservice.<namespace>.svc.cluster.local:80.
  kubetnl tunnel myservice 8080:80

  # Tunnel to 10.10.10.10:3333 from myservice.<namespace>.svc.cluster.local:80.
  kubetnl tunnel myservice 10.10.10.10:3333:80

  # Tunnel to local port 8080 from myservice.<namespace>.svc.cluster.local:80 and to local port 9090 from myservice.<namespace>.svc.cluster.local:90.
  kubetnl tunnel myservice 8080:80 9090:90

  # Tunnel to local port 80 from myservice.<namespace>.svc.cluster.local:80 using version 0.1.0 of the kubetnl server


Usage:
  kubetnl tunnel SERVICE_NAME TARGET_ADDR:SERVICE_PORT [...[TARGET_ADDR:SERVICE_PORT]] [flags] [options]

Use "kubetnl options" for a list of global command-line options (applies to all commands).
```

## Using the library

`kubetnl` can also be used for exposing your local services in a Kubernetes cluster programmatically.

Take a look at the [exposed_http_test.go](tests/exposed_http_test.go) example
for more information.

# Alternatives

See a [list of alternatives](docs/alternatives.md).
