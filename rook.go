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

var motion_map map[string]int

// Define a struct that matches the JSON payload structure
// {"encryption":false,"BTHome_version":2,"pid":198,"Battery":100,"Illuminance":0,"Motion":1,"addr":"e8:e0:7e:a6:ac:db","rssi":-56}

type Payload struct {
	Encryption    bool   `json:"encryption"`
	BTHomeVersion int    `json:"BTHome_version"`
	Pid           int    `json:"pid"`
	Battery       int    `json:"Battery"`
	Illuminance   int    `json:"Illuminance"`
	Motion        int    `json:"Motion"`
	Addr          string `json:"addr"`
	Rssi          int    `json:"rssi"`
}

type Status struct {
	Status string `json:"status"`
}

type Context struct {
	MessageCount int
}

var context *Context

// https://gorilla.github.io/
// https://github.com/samber/lo
// https://github.com/gin-gonic/gin - need so we can front w/ envoy
// https://github.com/avelino/awesome-go
// https://gobyexample.com/

func vard(x ...int) (int, error) {
	var y int
	for _, v := range x {
		y += v
	}
	return y, nil
}

func main() {
	var z, err = vard(1, 2, 3)
	if err == nil {
		fmt.Println(z)
	}
	context = &Context{MessageCount: 0}
	username := flag.String("username", "foo", "MQTT username")
	password := flag.String("password", "bar", "MQTT password")
	mqtt_hostname := flag.String("mqtt_server", "homeassistant.local", "MQTT hostname")
	mqtt_port := flag.Int("mqtt_port", 1883, "MQTT port")
	flag.Parse()

	motion_map = make(map[string]int)

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	opts := MQTT.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:%d", *mqtt_hostname, *mqtt_port))
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
	r.HandleFunc("/", indexHandler).Methods("GET")
	http.Handle("/", r)
	http.ListenAndServe(":8080", nil)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// w.Header().Set("Content-Type", "application/text")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Message Count %d", context.MessageCount)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var status Status
	status.Status = "ok"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	json.NewEncoder(w).Encode(status)
}

var messagePubHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
	context.MessageCount++

	// Parse the JSON payload
	var payload Payload
	err := json.Unmarshal(msg.Payload(), &payload)
	if err != nil {
		fmt.Printf("Error parsing JSON: %s\n", err)
		return
	}

	// Use the parsed data
	fmt.Printf("Parsed payload: %+v\n", payload)
	fmt.Printf("\tMotion: %d\n", payload.Motion)

	val, ok := motion_map[msg.Topic()]
	if (ok && val != payload.Motion) || !ok {
		fmt.Print("Send email\n")
	}

	motion_map[msg.Topic()] = payload.Motion
}

// TODO Parse motion detection and send email
func sub(client MQTT.Client) {
	topic := "mostert/motion/#"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	fmt.Printf("Subscribed to topic: %s\n", topic)
}
