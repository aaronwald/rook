package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	// LO "github.com/samber/lo"
)

// Define a struct that matches the JSON payload structure
type Payload struct {
	// Add fields according to your JSON structure
	Time  string `json:"time"`
	Model string `json:"model"`
	// Add more fields as needed
}

// https://gorilla.github.io/
// https://github.com/samber/lo
// https://github.com/gin-gonic/gin - need so we can front w/ envoy
// https://github.com/avelino/awesome-go

func main() {
	username := flag.String("username", "foo", "MQTT username")
	password := flag.String("password", "bar", "MQTT password")
	flag.Parse()

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	opts := MQTT.NewClientOptions().AddBroker("tcp://homeassistant.local:1883")
	opts.SetClientID("rook_" + hostname)
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.SetUsername(*username)
	opts.SetPassword(*password)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	sub(client)

	go startHTTPServer()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// TODO Add a little REST API to get status
	fmt.Println("Waiting for messages...")
	<-c
	fmt.Println("\nExiting gracefully...")

	client.Disconnect(250)
}

func startHTTPServer() {
	r := mux.NewRouter()
	r.HandleFunc("/status", statusHandler).Methods("GET")
	http.Handle("/", r)
	http.ListenAndServe(":8080", nil)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

var messagePubHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())

	// Parse the JSON payload
	var payload Payload
	err := json.Unmarshal(msg.Payload(), &payload)
	if err != nil {
		fmt.Printf("Error parsing JSON: %s\n", err)
		return
	}

	// Use the parsed data
	fmt.Printf("Parsed payload: %+v\n", payload)
}

// TODO Parse motion detection and send email
func sub(client MQTT.Client) {
	topic := "#"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	fmt.Printf("Subscribed to topic: %s\n", topic)
}
