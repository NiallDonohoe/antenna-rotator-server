package main

import (
	server "antenna-rotator-server/rotator-server"
	"fmt"
)

func main() {
	fmt.Println("Starting Server ...")
	s := server.CreateServer()
	s.StartServer()
}
