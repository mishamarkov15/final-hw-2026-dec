package main

import (
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		os.Exit(1)
	}
	switch os.Args[1] {
	case "tcp":
		conn, err := net.DialTimeout("tcp", os.Args[2], time.Second)
		if err != nil {
			os.Exit(1)
		}
		conn.Close()
	case "http":
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get(os.Args[2])
		if err != nil {
			os.Exit(1)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
	default:
		os.Exit(1)
	}
}
