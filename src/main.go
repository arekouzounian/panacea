package main

import (
	"flag"

	"github.com/arekouzounian/panacea/p2p"
)

var (
	PortFlag string
)

func main() {
	flag.StringVar(&PortFlag, "p", "8080", "the port to run the webserver on. default 8080")
	flag.Parse()

	p2p.StartPeer(PortFlag)
}
