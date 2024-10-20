package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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

func peerID() []byte {
	bytes := make([]byte, 20)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return bytes
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
	query.Add("peer_id", string(peerID()))
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

type HandshakeMessage struct {
	Protocol string
	Reserved [8]byte
	InfoHash []byte
	PeerID   []byte
}

func marshalHandshakeMessage(w io.Writer, m *HandshakeMessage) error {
	if _, err := w.Write([]byte{byte(len(m.Protocol))}); err != nil {
		return fmt.Errorf("write protocol length: %s", err)
	}

	if _, err := w.Write([]byte(m.Protocol)); err != nil {
		return fmt.Errorf("write protocol: %s", err)
	}

	if _, err := w.Write(m.Reserved[:]); err != nil {
		return fmt.Errorf("write reserved: %s", err)
	}

	if _, err := w.Write(m.InfoHash); err != nil {
		return fmt.Errorf("write info hash: %s", err)
	}

	if _, err := w.Write(m.PeerID); err != nil {
		return fmt.Errorf("write peer id: %s", err)
	}

	return nil
}

func unmarshalHandshakeMessage(r io.Reader, m *HandshakeMessage) error {
	reader := bufio.NewReader(r)

	protocolLength, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read protocol length: %s", err)
	}

	protocol := make([]byte, protocolLength)
	if _, err := io.ReadFull(reader, protocol); err != nil {
		return fmt.Errorf("read protocol: %s", err)
	}
	m.Protocol = string(protocol)

	if _, err := io.ReadFull(reader, m.Reserved[:]); err != nil {
		return fmt.Errorf("read reserved: %s", err)
	}

	m.InfoHash = make([]byte, 20)
	if _, err := io.ReadFull(reader, m.InfoHash); err != nil {
		return fmt.Errorf("read info hash: %s", err)
	}

	m.PeerID = make([]byte, 20)
	if _, err := io.ReadFull(reader, m.PeerID); err != nil {
		return fmt.Errorf("read peer id: %s", err)
	}

	return nil
}

func cmdHandshake() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		fmt.Println(err)
		return
	}

	peerAddr := os.Args[3]
	conn, err := net.Dial("tcp", peerAddr)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer conn.Close()

	m := HandshakeMessage{
		Protocol: "BitTorrent protocol",
		InfoHash: torrent.InfoHash(),
		PeerID:   peerID(),
	}
	if err := marshalHandshakeMessage(conn, &m); err != nil {
		fmt.Println(err)
		return
	}

	var response HandshakeMessage
	if err := unmarshalHandshakeMessage(conn, &response); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Peer ID: %x\n", response.PeerID)
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
	case "handshake":
		cmdHandshake()
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
