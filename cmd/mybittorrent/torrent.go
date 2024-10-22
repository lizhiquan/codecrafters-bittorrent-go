package main

import (
	"crypto/sha1"
	"os"

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

func NewTorrent(path string) (*Torrent, error) {
	torrentFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer torrentFile.Close()

	var torrent Torrent
	err = bencode.Unmarshal(torrentFile, &torrent)
	if err != nil {
		return nil, err
	}

	return &torrent, nil
}

func (t *TorrentInfo) Hash() []byte {
	hash := sha1.New()
	_ = bencode.Marshal(hash, *t)
	return hash.Sum(nil)
}

func (t *TorrentInfo) PieceHashes() [][]byte {
	var hashes [][]byte
	for i := 0; i < len(t.Pieces); i += 20 {
		hashes = append(hashes, []byte(t.Pieces[i:i+20]))
	}
	return hashes
}

func (t *Torrent) Peers() ([]string, error) {
	return getPeers(t.Announce, t.Info.Hash(), t.Info.Length)
}
