package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type AuthRequest struct {
	Token string `json:"token"`
}

type SignRequest struct {
	Message       string `json:"message"`
	WalletAddress string `json:"wallet_address"`
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func main() {
	// Fetch the token from the server
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MTY1MDU2MDJ9.XEQDJuHNRUKQhkN27IYvmJ7FFPi0atADCJE_SnLvhzU"
	token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MTY1MDYyOTF9.DzsbkB8nQpz55ypx3xmA_G3eqONloM5vwrvcza19e74"

	// Connect to the WebSocket server
	url := "ws://localhost:8180/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("Failed to connect to WebSocket server: %v", err)
	}
	defer conn.Close()

	// Authenticate with the server
	authReq := AuthRequest{Token: token}
	if err := conn.WriteJSON(authReq); err != nil {
		log.Fatalf("Failed to send auth message: %v", err)
	}

	// Listen for messages from the server
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("Failed to read message: %v", err)
				return
			}
			if msg.Type == "sign" {
				signReq := msg.Data.(map[string]interface{})
				fmt.Printf("Received message: %s from wallet: %s\n", signReq["message"], signReq["wallet_address"])

				// Send response back to the server
				response := Message{Type: "response", Data: signReq}
				if err := conn.WriteJSON(response); err != nil {
					log.Printf("Failed to send response: %v", err)
				}
			}
		}
	}()

	// Keep the client running
	select {}
}

func fetchToken() (string, error) {
	resp, err := http.Get("http://localhost:8180/generate-token")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.Token, nil
}
