package main

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

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

func cmdDecode() {
	bencodedValue := os.Args[2]

	decoded, err := bencode.Decode(strings.NewReader(bencodedValue))
	if err != nil {
		fmt.Println(err)
		return
	}

	jsonOutput, _ := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
}

func cmdInfo() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Tracker URL: %s\n", torrent.Announce)
	fmt.Printf("Length: %d\n", torrent.Info.Length)
	fmt.Printf("Info Hash: %x\n", torrent.InfoHash())
	fmt.Printf("Piece Length: %d\n", torrent.Info.PieceLength)
	fmt.Println("Piece Hashes:")
	for _, hash := range torrent.PieceHashes() {
		fmt.Println(hash)
	}
}

func cmdPeers() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		fmt.Println(err)
		return
	}

	req, err := http.NewRequest("GET", torrent.Announce, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	query := req.URL.Query()
	query.Add("info_hash", string(torrent.InfoHash()))
	query.Add("peer_id", "00112233445566778899")
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", strconv.Itoa(torrent.Info.Length))
	query.Add("compact", "1")
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer resp.Body.Close()

	var response TrackerResponse
	err = bencode.Unmarshal(resp.Body, &response)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, peer := range response.PeerList() {
		fmt.Println(peer)
	}
}

func main() {
	command := os.Args[1]

	switch command {
	case "decode":
		cmdDecode()
	case "info":
		cmdInfo()
	case "peers":
		cmdPeers()
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
