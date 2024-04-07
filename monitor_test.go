package main

import (
	"bytes"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type LogHook struct {
	mqtt.HookBase
}

func (h *LogHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnPublished,
		mqtt.OnConnect,
		mqtt.OnDisconnect,
		mqtt.OnSubscribed,
		mqtt.OnUnsubscribed,
	}, []byte{b})
}

func (h *LogHook) OnPublished(client *mqtt.Client, pkt packets.Packet) {
	h.Log.Info("publish", "topic", pkt.TopicName, "payload", string(pkt.Payload), "client", client.ID)
}

// hook.OnDisconnect(cl, err, expire)
func (h *LogHook) OnConnect(client *mqtt.Client, pkt packets.Packet) error {
	h.Log.Info("client connected", "client", client.ID)
	return nil
}

// client *mqtt.Client, err error, expire bool
func (h *LogHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	h.Log.Info("client disconnected", "client", cl.ID, "expire", expire, "error", err)
}

func (h *LogHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reasonCodes []byte) {
	h.Log.Info("client subscribed", "client", cl.ID, "topic", pk.TopicName)
}

func (h *LogHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
	h.Log.Info("client unsubscribed", "client", cl.ID, "topic", pk.TopicName)
}
