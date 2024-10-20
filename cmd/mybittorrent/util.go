package main

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"strconv"

	bencode "github.com/jackpal/bencode-go"
)

func parseTorrentFile(path string) (*Torrent, error) {
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

func peerID() []byte {
	bytes := make([]byte, 20)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return bytes
}

func getPeers(torrent *Torrent) ([]string, error) {
	req, err := http.NewRequest("GET", torrent.Announce, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	query := req.URL.Query()
	query.Add("info_hash", string(torrent.InfoHash()))
	query.Add("peer_id", string(peerID()))
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", strconv.Itoa(torrent.Info.Length))
	query.Add("compact", "1")
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	defer resp.Body.Close()

	var response TrackerResponse
	err = bencode.Unmarshal(resp.Body, &response)
	if err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return response.PeerList(), nil
}
