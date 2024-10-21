package main

import (
	"encoding/hex"
	"net/url"
	"strings"
)

type Magnet struct {
	TrackerURL string
	InfoHash   []byte
}

func NewMagnet(magnet string) (*Magnet, error) {
	u, err := url.Parse(magnet)
	if err != nil {
		return nil, err
	}

	q := u.Query()

	hash, err := hex.DecodeString(strings.TrimPrefix(q.Get("xt"), "urn:btih:"))
	if err != nil {
		return nil, err
	}

	return &Magnet{
		TrackerURL: q.Get("tr"),
		InfoHash:   hash,
	}, nil
}

func (m *Magnet) Peers() ([]string, error) {
	return getPeers(m.TrackerURL, m.InfoHash, 1)
}
