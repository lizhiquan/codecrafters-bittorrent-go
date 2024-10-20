package main

import (
	"encoding/binary"
	"fmt"
	"net"
)

type TrackerResponse struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

func (r *TrackerResponse) PeerList() []string {
	var peers []string
	for i := 0; i < len(r.Peers); i += 6 {
		ip := net.IP(r.Peers[i : i+4])
		port := binary.BigEndian.Uint16([]byte(r.Peers[i+4 : i+6]))
		peers = append(peers, fmt.Sprintf("%s:%d", ip, port))
	}
	return peers
}
