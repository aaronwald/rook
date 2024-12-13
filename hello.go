package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	// Define command line flags
	username := flag.String("username", "foo", "MQTT username")
	password := flag.String("password", "bar", "MQTT password")
	flag.Parse()

	opts := MQTT.NewClientOptions().AddBroker("tcp://homeassistant.local:1883")
	opts.SetClientID("go_mqtt_client")
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.SetUsername(*username)
	opts.SetPassword(*password)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	sub(client)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Waiting for Ctrl+C...")
	<-c
	fmt.Println("\nCtrl+C received. Exiting gracefully...")

	client.Disconnect(250)

}

var messagePubHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

func sub(client MQTT.Client) {
	topic := "#"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	fmt.Printf("Subscribed to topic: %s\n", topic)
}
