package main

import (
	"crypto/sha1"
	"fmt"

	bencode "github.com/jackpal/bencode-go"
)

type TorrentInfo struct {
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
	PieceLength int    `bencode:"piece length"`
	Pieces      string `bencode:"pieces"`
}

type Torrent struct {
	Announce string      `bencode:"announce"`
	Info     TorrentInfo `bencode:"info"`
}

func (t *Torrent) InfoHash() []byte {
	hash := sha1.New()
	_ = bencode.Marshal(hash, t.Info)
	return hash.Sum(nil)
}

func (t *Torrent) PieceHashes() []string {
	var hashes []string
	for i := 0; i < len(t.Info.Pieces); i += 20 {
		hashes = append(hashes, fmt.Sprintf("%x", t.Info.Pieces[i:i+20]))
	}
	return hashes
}
