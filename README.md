# Whippet - a request/response cli tool for MQTT v5

Whippet is a simple command line tool for sending requests and receiving responses over MQTT v5.

![Whippet](whippet-pixels.webp)

Typically, you'll use whippet to request something over MQTT and get a response back. Whippet will take care of 
generating and matching up MQTT correlation IDs as well as setting the response topic for the request.

Whippets secondary use case is a library for performing MQTT request/response operations. This is why a lot of the 
logic is in the whippet package.

Whippet has a wrapper around the paho MQTT client. Its use is optional, naturally. You can just provide your own paho client. 

## Usage

```shell
$ echo "ping" | whippet -server tcp://localhost:1883 -topic "my/topic" -qos 1
pong
```

The message to be sent is read from `stdin` and the response is written to `stdout`. Any error messages
are written to `stderr`.

### Options

# Application Help Guide

Below are the command-line options available for the application:

- **-clientID** `string`  
  Specifies a clientID for the connection.  
  Default: `$hostname-$random`

- **-help**  
  Prints this help message.

- **-password** `string`  
  Password to match username.

- **-qos** `int`  
  The Quality of Service (QoS) level to send the messages at.  
  Default: `1`

- **-response-topic** `string`  
  Topic the other party should respond to. Defaults to the publish topic + `/response`.

- **-retained**  
  Specifies if the messages are sent with the retained flag.

- **-server** `string`  
  The full URL of the MQTT server to connect to.  
  Default: `localhost:1883`

- **-timeout** `duration`  
  Timeout for the operation.  
  Default: `10s`

- **-topic** `string`  
  Topic to publish the messages on.

- **-username** `string`  
  A username to authenticate to the MQTT server.


### Development

This is quite a simple tool and I made it to scratch my itch. If you need anything in it, 
feel free to open an issue or a PR.

The one thing I'll likely add is support for client TLS certs.

Note that the tests fire up a Mochi MQTT broker on port 1883. This is why we have a dependency on
`github.com/mochi-co/mqtt`. 
