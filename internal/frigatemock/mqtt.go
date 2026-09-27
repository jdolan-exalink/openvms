package frigatemock

import (
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTPublisher publishes review messages and the availability topic Frigate maintains.
type MQTTPublisher struct {
	client mqtt.Client
	prefix string
}

func ConnectMQTT(url, clientID, prefix string, log *slog.Logger) (*MQTTPublisher, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(url).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2*time.Second).
		SetWill(prefix+"/available", "offline", 1, true).
		SetOnConnectHandler(func(c mqtt.Client) {
			c.Publish(prefix+"/available", 1, true, "online")
			log.Info("mqtt connected", "broker", url)
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Warn("mqtt connection lost", "error", err)
		})
	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(10*time.Second) && tok.Error() == nil {
		log.Warn("mqtt broker not reachable yet, retrying in background", "broker", url)
	} else if tok.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", tok.Error())
	}
	return &MQTTPublisher{client: c, prefix: prefix}, nil
}

func (p *MQTTPublisher) Publish(topic string, payload []byte) error {
	tok := p.client.Publish(topic, 1, false, payload)
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish %s: timeout", topic)
	}
	return tok.Error()
}

func (p *MQTTPublisher) Close() {
	p.client.Publish(p.prefix+"/available", 1, true, "offline").WaitTimeout(2 * time.Second)
	p.client.Disconnect(500)
}
