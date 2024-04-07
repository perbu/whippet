# Whippet - a request/response cli tool for MQTT v5

Whippet is a simple command line tool for sending requests and receiving responses over MQTT v5.

![Whippet](whippet-pixels.webp)

Typically, you'll use whippet to request something over MQTT and get a response back.


## Options

`-broker` - The URL of the MQTT broker.

`-mdns` - The mDNS name of the MQTT broker service

`-t,--topic` - The topic to publish to. 

`-qos` - The QoS level to use. Default: `1`

`-message` - The message to send. 

`-client-id` - The client ID to use. Default: `whippet`

`-help` - Show this help message

