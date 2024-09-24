package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/websocket"
)

type SignRequest struct {
	Message       string `json:"message"`
	WalletAddress string `json:"wallet_address"`
}

type AuthRequest struct {
	Token string `json:"token"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

var (
	upgrader    = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	clients     = make(map[*websocket.Conn]string)  // Map of websocket connections to their tokens
	connections = make(map[string]*websocket.Conn)  // Map of tokens to websocket connections
	responses   = make(map[string]chan SignRequest) // Map of tokens to channels for responses
	clientLock  = sync.Mutex{}
	jwtKey      = []byte("my_secret_key")
)

func main() {
	http.HandleFunc("/generate-token", generateTokenHandler)
	http.HandleFunc("/sign", signHandler)
	http.HandleFunc("/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8180", nil))
}

func generateTokenHandler(w http.ResponseWriter, r *http.Request) {
	expirationTime := time.Now().Add(1 * time.Hour)
	claims := &jwt.StandardClaims{
		ExpiresAt: expirationTime.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	resp := TokenResponse{Token: tokenString}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func signHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	tokenString := r.Header.Get("Authorization")
	if tokenString == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	var req SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	clientLock.Lock()
	conn, ok := connections[tokenString]
	clientLock.Unlock()

	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	responseChan := make(chan SignRequest)
	clientLock.Lock()
	responses[tokenString] = responseChan
	clientLock.Unlock()

	defer func() {
		clientLock.Lock()
		delete(responses, tokenString)
		clientLock.Unlock()
	}()

	// Broadcast the message to the WebSocket client
	message := Message{Type: "sign", Data: req}
	if err := conn.WriteJSON(message); err != nil {
		http.Error(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	select {
	case response := <-responseChan:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	case <-time.After(10 * time.Second):
		http.Error(w, "Request timeout", http.StatusGatewayTimeout)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Upgrade:", err)
		return
	}
	defer conn.Close()

	auth := make(chan AuthRequest)
	go authHandler(conn, auth)

	select {
	case authReq := <-auth:
		clientLock.Lock()
		clients[conn] = authReq.Token
		connections[authReq.Token] = conn
		clientLock.Unlock()
	case <-time.After(10 * time.Second):
		log.Println("Auth timeout")
		conn.Close()
		return
	}

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			clientLock.Lock()
			delete(clients, conn)
			delete(connections, clients[conn])
			clientLock.Unlock()
			log.Println("Read:", err)
			break
		}

		// Process the message received from WebSocket client
		if msg.Type == "response" {
			clientLock.Lock()
			responseChan, ok := responses[clients[conn]]
			clientLock.Unlock()

			if ok {
				responseData, ok := msg.Data.(map[string]interface{})
				if !ok {
					log.Println("Invalid response data")
					continue
				}
				signReq := SignRequest{
					Message:       responseData["message"].(string),
					WalletAddress: responseData["wallet_address"].(string),
				}
				responseChan <- signReq
			}
		}
	}
}

func authHandler(conn *websocket.Conn, auth chan AuthRequest) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Auth Read:", err)
			break
		}
		var authReq AuthRequest
		if err := json.Unmarshal(message, &authReq); err != nil {
			log.Println("Auth Unmarshal:", err)
			continue
		}

		// Verify the token
		valid, err := verifyToken(authReq.Token)
		if !valid || err != nil {
			log.Println("Invalid Token:", err)
			conn.WriteMessage(websocket.TextMessage, []byte("Invalid Token"))
			conn.Close()
			return
		}

		auth <- authReq
		return
	}
}

func verifyToken(tokenString string) (bool, error) {
	claims := &jwt.StandardClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})
	if err != nil {
		return false, err
	}
	if !token.Valid {
		return false, errors.New("invalid token")
	}
	if claims.ExpiresAt < time.Now().Unix() {
		return false, errors.New("token expired")
	}

	return true, nil
}
