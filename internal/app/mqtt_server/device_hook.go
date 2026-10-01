package mqtt_server

import (
	"fmt"
	"strings"
	"time"

	mqttServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	client "xiaozhi-esp32-server-golang/internal/data/msg"
	log "xiaozhi-esp32-server-golang/logger"
)

// DeviceHook device ACL and auto-subscribe hook
// Normal users cannot subscribe freely; may publish only allowed topics; auto-subscribe /p2p/device_sub/{mac} on connect
type DeviceHook struct {
	mqttServer.HookBase
	server           *mqttServer.Server
	publishLifecycle func(event client.MqttLifecycleEvent) error
}

func (h *DeviceHook) ID() string {
	return "custom-device-hook"
}

func (h *DeviceHook) Provides(b byte) bool {
	return b == mqttServer.OnDisconnect || b == mqttServer.OnACLCheck || b == mqttServer.OnSessionEstablished || b == mqttServer.OnSubscribe || b == mqttServer.OnPublish
}

// OnACLCheck publish/subscribe ACL
func (h *DeviceHook) OnACLCheck(cl *mqttServer.Client, topic string, write bool) bool {
	isAdmin := isAdminUser(cl)

	if isAdmin {
		return true // Super admin has no limits
	}

	if write {
		// Normal users may publish only to "device-server"
		if topic == client.MDeviceMockPubTopicPrefix {
			return true
		}
		log.Warnf("Forbid normal user publish to %s", topic)
		return false
	}

	mac := parseMacFromClientId(cl.ID)
	if mac == "" {
		log.Warnf("Forbid normal user subscribe %s: cannot parse MAC from client ID, clientID=%s", topic, cl.ID)
		return false
	}

	allowedTopic := deviceSubTopic(mac)
	if topic == allowedTopic {
		return true
	}

	log.Warnf("Forbid normal user subscribe %s: only own topic %s allowed", topic, allowedTopic)
	return false
}

func (h *DeviceHook) OnConnect(cl *mqttServer.Client, pk packets.Packet) error {
	isAdmin := isAdminUser(cl)
	if isAdmin {
		return nil
	}
	pk.Connect.Clean = true
	return nil
}

func (h *DeviceHook) OnDisconnect(cl *mqttServer.Client, err error, ok bool) {
	if cl == nil {
		log.Warnf("OnDisconnect: client is nil, err=%v, ok=%v", err, ok)
		return
	}
	isAdmin := isAdminUser(cl)
	mac := parseMacFromClientId(cl.ID)
	deviceID := deviceIDFromClientId(cl.ID)
	takenOver := cl.IsTakenOver()

	log.Infof("OnDisconnect: clientID=%s, deviceID=%s, mac=%s, ok=%v, err=%v, takenOver=%v, isAdmin=%v",
		cl.ID, deviceID, mac, ok, err, takenOver, isAdmin)

	if isAdmin {
		return
	}
	if takenOver {
		log.Infof("Client %s taken over by new connection with same ID, skip unsubscribe and offline lifecycle publish", cl.ID)
		return
	}
	if mac == "" {
		log.Infof("OnDisconnect: failed to parse MAC from client ID, clientID=%s, err=%v, ok=%v", cl.ID, err, ok)
		return
	}

	log.Infof("OnDisconnect: preparing to publish offline lifecycle, clientID=%s, deviceID=%s", cl.ID, deviceID)
	h.publishLifecycleEvent(cl.ID, client.MqttLifecycleStateOffline)
	topic := deviceSubTopic(mac)

	action := h.server.Topics.Unsubscribe(topic, cl.ID)
	log.Infof("OnDisconnect: unsubscribe client %s from topic %s, action=%v", cl.ID, topic, action)

	return
}

// OnSessionEstablished auto-subscribe after session is established
func (h *DeviceHook) OnSessionEstablished(cl *mqttServer.Client, pk packets.Packet) {
	isAdmin := isAdminUser(cl)
	mac := parseMacFromClientId(cl.ID)
	deviceID := deviceIDFromClientId(cl.ID)
	if isAdmin {
		return // No limits for super admin
	}
	if mac == "" {
		log.Info("Warning: failed to parse MAC address from client ID:", cl.ID)
		return
	}
	log.Infof("OnSessionEstablished: clientID=%s, deviceID=%s, mac=%s, clean=%v", cl.ID, deviceID, mac, pk.Connect.Clean)
	h.publishLifecycleEvent(cl.ID, client.MqttLifecycleStateOnline)

	topic := deviceSubTopic(mac)

	// Subscribe via server API instead of injecting packets
	clientID := cl.ID
	exists := h.server.Topics.Subscribe(clientID, packets.Subscription{
		Filter: topic,
		Qos:    0,
	})

	log.Infof("Subscribe client %s to topic %s, exists: %v", clientID, topic, exists)
}

// OnSubscribe log subscribe packets
func (h *DeviceHook) OnSubscribe(cl *mqttServer.Client, pk packets.Packet) packets.Packet {
	log.Info("=== Received subscribe packet ===")
	log.Infof("Client ID: %s", cl.ID)
	log.Infof("Packet type: %v", pk.FixedHeader.Type)
	log.Infof("Packet ID: %d", pk.PacketID)

	if len(pk.Filters) > 0 {
		log.Info("Subscription info:")
		for i, sub := range pk.Filters {
			log.Infof("  %d. Topic: %s, QoS: %d", i+1, sub.Filter, sub.Qos)
		}
	}

	log.Info("==================")
	return pk
}

// OnPublish log publish packets
func (h *DeviceHook) OnPublish(cl *mqttServer.Client, pk packets.Packet) (packets.Packet, error) {
	if cl == nil {
		return pk, nil
	}

	log.Info("=== Received publish packet ===")
	log.Infof("Client ID: %s", cl.ID)
	log.Infof("Packet type: %v", pk.FixedHeader.Type)
	log.Infof("Packet ID: %d", pk.PacketID)
	log.Infof("Topic: %s", pk.TopicName)

	if isAdminUser(cl) {
		return pk, nil
	}

	if len(pk.Payload) > 0 {
		if len(pk.Payload) > 100 {
			// If message is long, show only the first 100 bytes
			log.Infof("Message payload (first 100 bytes): %s...", pk.Payload[:100])
		} else {
			log.Infof("Message payload: %s", pk.Payload)
		}
	} else {
		log.Info("Message payload: <empty>")
	}

	//Find MAC address from cl
	mac := parseMacFromClientId(cl.ID)
	if mac == "" {
		log.Info("Warning: failed to parse MAC address from client ID:", cl.ID)
		return pk, nil
	}
	forwardTopic := fmt.Sprintf("%s%s", client.MDevicePubTopicPrefix, mac)

	pk.TopicName = forwardTopic

	log.Info("==================")
	return pk, nil
}

// Check whether client is super admin
func isAdminUser(cl *mqttServer.Client) bool {
	if cl == nil {
		return false
	}
	return string(cl.Properties.Username) == configuredAdminUsername()
}

// Parse clientId for MAC address
func parseMacFromClientId(clientId string) string {
	parts := strings.Split(clientId, "@@@")
	if len(parts) >= 3 {
		return parts[1]
	}
	return ""
}

func deviceIDFromClientId(clientID string) string {
	mac := parseMacFromClientId(clientID)
	if mac == "" {
		return ""
	}
	return strings.ReplaceAll(mac, "_", ":")
}

func (h *DeviceHook) publishLifecycleEvent(clientID string, state string) {
	if h == nil || h.publishLifecycle == nil {
		return
	}
	deviceID := deviceIDFromClientId(clientID)
	if deviceID == "" {
		log.Warnf("Skip publishing MQTT lifecycle event: cannot parse deviceID, clientID=%s, state=%s", clientID, state)
		return
	}
	event := client.MqttLifecycleEvent{
		Type:     client.MqttLifecycleType,
		DeviceID: deviceID,
		State:    state,
		ClientID: clientID,
		Ts:       time.Now().UnixMilli(),
	}
	log.Infof("Publish MQTT lifecycle event: device=%s, clientID=%s, state=%s, ts=%d", deviceID, clientID, state, event.Ts)
	if err := h.publishLifecycle(event); err != nil {
		log.Warnf("Failed to publish MQTT lifecycle event: device=%s state=%s err=%v", deviceID, state, err)
	}
}

func deviceSubTopic(mac string) string {
	return fmt.Sprintf("%s%s", client.MDeviceSubTopicPrefix, mac)
}

// Start periodic task that prints subscribed topics
func (h *DeviceHook) StartPeriodicSubscriptionPrinter(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			h.PrintAllClientSubscriptions()
		}
	}()
}

// Print subscribed topics for all clients
func (h *DeviceHook) PrintAllClientSubscriptions() {
	log.Info("=== Client subscribed topic list ===")
	clients := h.server.Clients.GetAll()
	if len(clients) == 0 {
		log.Info("No connected clients")
		return
	}

	for clientID, _ := range clients {
		log.Infof("Topics subscribed by client %s: ", clientID)

		// Use server.Topics.Subscribers("+") for all topic subscribers
		// Then filter subscriptions matching this clientID
		allSubs := h.server.Topics.Subscribers("+")
		foundTopics := false

		// Check client subscriptions
		if subs, ok := allSubs.Subscriptions[clientID]; ok {
			log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
			foundTopics = true
		}

		// Check more possible topic subscriptions
		allSubs = h.server.Topics.Subscribers("#")
		if subs, ok := allSubs.Subscriptions[clientID]; ok {
			log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
			foundTopics = true
		}

		// Also check specific topics
		mac := parseMacFromClientId(clientID)
		if mac != "" {
			topic := deviceSubTopic(mac)
			topicSubs := h.server.Topics.Subscribers(topic)
			if subs, ok := topicSubs.Subscriptions[clientID]; ok {
				log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
				foundTopics = true
			}
		}

		if !foundTopics {
			log.Info("  No subscribed topics or unable to get them")
		}
	}
	log.Info("=====================")
}
